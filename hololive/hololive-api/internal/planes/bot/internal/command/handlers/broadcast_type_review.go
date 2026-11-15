package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
)

type broadcastVideoReview struct {
	ChannelID    string             `json:"channel_id"`
	Type         broadcasttype.Type `json:"type"`
	ReviewedAt   string             `json:"reviewed_at"`
	SourceURL    string             `json:"source_url"`
	EvidenceKind string             `json:"evidence_kind"`
	Evidence     string             `json:"evidence"`
}

// BroadcastTypeDecision은 Gemini Decisions API가 고른 유형과 그 확률이다. 영값은 답이 없다는 뜻이다.
type BroadcastTypeDecision struct {
	Type       broadcasttype.Type
	Confidence float64
}

// BroadcastTypeSignals는 제목·주제 밖에서 얻은 유형 근거다. PlayerMembersOnly는 방송이 끝난 뒤 YouTube player가 다시보기를
// 멤버 전용이라고 확인했다는 뜻이다.
type BroadcastTypeSignals struct {
	PlayerMembersOnly bool
	Decision          BroadcastTypeDecision
}

const minBroadcastTypeDecisionConfidence = 0.3

// ClassifyBroadcastVideo는 제목·주제 판정에 영상별 검토 근거, player 멤버 전용 확인, Gemini 판정을 더하며 외부 조회나 상태 변경을 하지 않는다.
// 미분류이면서 영상 ID와 채널 ID가 모두 일치할 때 검토 결과를 사용한다.
// 검토에서 확인한 멤버 전용 영상은 일반 유형보다 우선하며 기존 멤버십 판정은 유지한다.
// YouTube player의 멤버 전용 확인과 Gemini 판정은 규칙과 검토가 모두 미분류로 남긴 방송에만 쓴다. 이 확인에는 공개 방송의 다시보기를
// 나중에 제한한 경우도 들어 있어 형식 판정을 바꾸지 않으며, 미분류에서는 접근 범위가 형식보다 앞서므로 Gemini보다 먼저 본다.
// Gemini 판정의 확률이 문턱보다 낮으면 미분류로 둔다.
func ClassifyBroadcastVideo(videoID, channelID, topicID, title string, signals BroadcastTypeSignals) BroadcastClassification {
	classification := ClassifyBroadcastWithSource(topicID, title)
	review, ok := broadcastRules.ReviewedVideos[videoID]

	if ok && review.ChannelID == channelID &&
		(classification.Type == broadcasttype.Unknown ||
			(review.Type == broadcasttype.Membership && classification.Type != broadcasttype.Membership)) {
		return BroadcastClassification{Type: review.Type, Source: "reviewed"}
	}

	if classification.Type != broadcasttype.Unknown {
		return classification
	}

	if signals.PlayerMembersOnly {
		return BroadcastClassification{Type: broadcasttype.Membership, Source: "player"}
	}

	if decision := signals.Decision; decision.Type != broadcasttype.Unknown &&
		knownBroadcastType(decision.Type) && decision.Confidence >= minBroadcastTypeDecisionConfidence {
		return BroadcastClassification{Type: decision.Type, Source: "gemini"}
	}

	return classification
}

func validateBroadcastVideoReviews(reviews map[string]broadcastVideoReview) error {
	for videoID, review := range reviews {
		if !validYouTubeVideoID(videoID) || videoID != strings.TrimSpace(videoID) {
			return fmt.Errorf("review uses invalid video ID %q", videoID)
		}

		if len(review.ChannelID) != 24 || !strings.HasPrefix(review.ChannelID, "UC") {
			return fmt.Errorf("review %q requires a YouTube channel ID", videoID)
		}

		if !knownBroadcastType(review.Type) || review.Type == broadcasttype.Unknown {
			return fmt.Errorf("review %q uses unclassified type %q", videoID, review.Type)
		}

		if _, err := time.Parse(time.RFC3339, review.ReviewedAt); err != nil {
			return fmt.Errorf("review %q has invalid review time: %w", videoID, err)
		}

		if review.SourceURL != "https://www.youtube.com/watch?v="+videoID ||
			strings.TrimSpace(review.EvidenceKind) == "" || strings.TrimSpace(review.Evidence) == "" {
			return fmt.Errorf("review %q requires matching source URL and evidence", videoID)
		}
	}

	return nil
}

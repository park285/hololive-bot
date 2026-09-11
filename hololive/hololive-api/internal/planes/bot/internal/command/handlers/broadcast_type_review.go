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

// ClassifyBroadcastVideo는 제목·주제 판정에 영상별 검토 근거를 더하며 외부 조회나 상태 변경을 하지 않는다.
// 미분류이면서 영상 ID와 채널 ID가 모두 일치할 때 검토 결과를 사용한다.
// 검토에서 확인한 멤버 전용 영상은 일반 유형보다 우선하며 기존 멤버십 판정은 유지한다.
func ClassifyBroadcastVideo(videoID, channelID, topicID, title string) BroadcastClassification {
	classification := ClassifyBroadcastWithSource(topicID, title)
	review, ok := broadcastRules.ReviewedVideos[videoID]

	if !ok || review.ChannelID != channelID {
		return classification
	}

	if classification.Type == broadcasttype.Unknown ||
		(review.Type == broadcasttype.Membership && classification.Type != broadcasttype.Membership) {
		return BroadcastClassification{Type: review.Type, Source: "reviewed"}
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

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

type BroadcastTypeDecision struct {
	Type       broadcasttype.Type
	Confidence float64
}

type BroadcastTypeSignals struct {
	PlayerMembersOnly bool
	Decision          BroadcastTypeDecision
}

const minBroadcastTypeDecisionConfidence = 0.3

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

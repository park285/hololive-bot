package observation

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/alarmtiming"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const alarmLatencyExceededThresholdMillis = alarmtiming.LatencyExceededThresholdMillis

// --- shared helpers ---

func normalizeRecord(record *domain.YouTubeContentAlarmTracking) (*domain.YouTubeContentAlarmTracking, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(record.Kind, record.ContentID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if record.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected_at is empty")
	}

	actualPublishedAt := yttimestamp.NormalizePtr(record.ActualPublishedAt)
	timing := alarmtiming.Build(actualPublishedAt, record.AlarmSentAt)
	actualPublishedAt = timing.ActualPublishedAt
	alarmSentAt := timing.AlarmSentAt
	latencyMillis := timing.AlarmLatencyMillis
	latencyExceeded := timing.AlarmLatencyExceeded
	canonicalContentID := canonicalTrackingIdentity(normalizedKind, normalizedContentID)

	return &domain.YouTubeContentAlarmTracking{
		Kind:                 normalizedKind,
		ContentID:            normalizedContentID,
		CanonicalContentID:   canonicalContentID,
		ChannelID:            normalizedChannelID,
		ActualPublishedAt:    actualPublishedAt,
		DetectedAt:           yttimestamp.Normalize(record.DetectedAt),
		AlarmSentAt:          alarmSentAt,
		AlarmLatencyMillis:   latencyMillis,
		AlarmLatencyExceeded: latencyExceeded,
		DeliveryStatus:       domain.ResolveYouTubeContentAlarmDeliveryStatus(alarmSentAt),
	}, nil
}

func buildDeliveryStatusExpr(alarmSentExpr string) string {
	return fmt.Sprintf(`CASE
	        WHEN %s IS NULL THEN '%s'
	        ELSE '%s'
	    END`,
		alarmSentExpr,
		domain.YouTubeContentAlarmDeliveryStatusPending,
		domain.YouTubeContentAlarmDeliveryStatusSent,
	)
}

func trackingCanonicalKey(kind domain.OutboxKind, canonicalContentID string) string {
	return string(kind) + "\x00" + strings.TrimSpace(canonicalContentID)
}

func mergeNormalizedTrackingRecord(existing *domain.YouTubeContentAlarmTracking, next *domain.YouTubeContentAlarmTracking) *domain.YouTubeContentAlarmTracking {
	if existing == nil {
		return next
	}
	if next == nil {
		return existing
	}

	merged := *existing
	mergeTrackingRecordFields(&merged, next)

	return normalizeMergedTrackingRecord(merged)
}

func mergeTrackingRecordFields(merged *domain.YouTubeContentAlarmTracking, next *domain.YouTubeContentAlarmTracking) {
	if strings.TrimSpace(next.ChannelID) != "" {
		merged.ChannelID = next.ChannelID
	}
	if next.ActualPublishedAt != nil {
		merged.ActualPublishedAt = next.ActualPublishedAt
	}
	if next.DetectedAt.Before(merged.DetectedAt) {
		merged.DetectedAt = next.DetectedAt
	}

	mergeTrackingAlarmSentAt(merged, next.AlarmSentAt)
}

func mergeTrackingAlarmSentAt(merged *domain.YouTubeContentAlarmTracking, nextAlarmSentAt *time.Time) {
	switch {
	case merged.AlarmSentAt == nil:
		merged.AlarmSentAt = nextAlarmSentAt
	case nextAlarmSentAt != nil && nextAlarmSentAt.Before(*merged.AlarmSentAt):
		merged.AlarmSentAt = nextAlarmSentAt
	}
}

func normalizeMergedTrackingRecord(merged domain.YouTubeContentAlarmTracking) *domain.YouTubeContentAlarmTracking {
	timing := alarmtiming.Build(merged.ActualPublishedAt, merged.AlarmSentAt)
	merged.ActualPublishedAt = timing.ActualPublishedAt
	merged.AlarmSentAt = timing.AlarmSentAt
	merged.AlarmLatencyMillis = timing.AlarmLatencyMillis
	merged.AlarmLatencyExceeded = timing.AlarmLatencyExceeded
	merged.DeliveryStatus = domain.ResolveYouTubeContentAlarmDeliveryStatus(merged.AlarmSentAt)

	return &merged
}

func normalizeIdentity(kind domain.OutboxKind, contentID string) (domain.OutboxKind, string, error) {
	normalizedContentID := strings.TrimSpace(contentID)
	if normalizedContentID == "" {
		return "", "", fmt.Errorf("content id is empty")
	}

	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		return kind, normalizedContentID, nil
	default:
		return "", "", fmt.Errorf("unsupported tracking kind: %s", kind)
	}
}

func trackingIdentityCandidates(kind domain.OutboxKind, contentID string) []string {
	normalizedContentID := strings.TrimSpace(contentID)
	switch kind {
	case domain.OutboxKindNewShort:
		canonicalContentID := canonicalTrackingIdentity(kind, normalizedContentID)
		rawContentID, err := ytcontentid.NormalizeShortVideoID(normalizedContentID)
		return trackingIdentityCandidatePair(canonicalContentID, rawContentID, err)
	case domain.OutboxKindCommunityPost:
		canonicalContentID := canonicalTrackingIdentity(kind, normalizedContentID)
		rawContentID, err := ytcontentid.NormalizeCommunityPostID(normalizedContentID)
		return trackingIdentityCandidatePair(canonicalContentID, rawContentID, err)
	default:
		return []string{normalizedContentID}
	}
}

func trackingIdentityCandidatePair(canonicalContentID string, rawContentID string, err error) []string {
	if err != nil || strings.TrimSpace(rawContentID) == "" {
		return []string{canonicalContentID}
	}
	if canonicalContentID == rawContentID {
		return []string{canonicalContentID}
	}

	return []string{canonicalContentID, rawContentID}
}

func canonicalTrackingIdentity(kind domain.OutboxKind, contentID string) string {
	normalizedContentID := strings.TrimSpace(contentID)
	canonicalContentID, err := ytcontentid.ForOutboxKind(kind, normalizedContentID)
	if err != nil {
		return normalizedContentID
	}
	return canonicalContentID
}

func buildLatencyMillisExpr(startExpr string, endExpr string) string {
	return fmt.Sprintf(`CASE
		        WHEN (%s) IS NULL OR (%s) IS NULL THEN NULL
		        ELSE CAST(ROUND(EXTRACT(EPOCH FROM (((%s)) - ((%s)))) * 1000) AS BIGINT)
		    END`, startExpr, endExpr, endExpr, startExpr)
}

func buildLatencyExceededExpr(latencyMillisExpr string) string {
	return fmt.Sprintf(`CASE
		        WHEN (%s) IS NULL THEN NULL
		        WHEN (%s) > %d THEN TRUE
		        ELSE FALSE
		    END`, latencyMillisExpr, latencyMillisExpr, alarmLatencyExceededThresholdMillis)
}

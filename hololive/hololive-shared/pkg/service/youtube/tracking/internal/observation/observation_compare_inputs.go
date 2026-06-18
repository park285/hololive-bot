package observation

import (
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

type ObservationPostComparisonInput struct {
	Kind              domain.OutboxKind `json:"kind"`
	AlarmType         domain.AlarmType  `json:"alarm_type"`
	CanonicalPostID   string            `json:"canonical_post_id"`
	ContentID         string            `json:"content_id,omitempty"`
	ChannelID         string            `json:"channel_id"`
	TitleHint         string            `json:"title_hint,omitempty"`
	ActualPublishedAt *time.Time        `json:"actual_published_at,omitempty"`
	DetectedAt        *time.Time        `json:"detected_at,omitempty"`
	AlarmSentAt       *time.Time        `json:"alarm_sent_at,omitempty"`
}

func BuildObservationPostComparisonInputsFromBaselines(
	rows []domain.YouTubeCommunityShortsObservationPostBaseline,
) []ObservationPostComparisonInput {
	inputs := make([]ObservationPostComparisonInput, 0, len(rows))
	for i := range rows {
		detectedAt := rows[i].DetectedAt
		inputs = append(inputs, normalizeObservationPostComparisonInput(
			rows[i].Kind,
			rows[i].PostID,
			"",
			rows[i].ChannelID,
			rows[i].ActualPublishedAt,
			&detectedAt,
			nil,
		))
	}
	return inputs
}

func BuildObservationPostComparisonInputsFromSentHistories(
	kind domain.OutboxKind,
	rows []ObservationAlarmSentHistoryRow,
) []ObservationPostComparisonInput {
	inputs := make([]ObservationPostComparisonInput, 0, len(rows))
	for i := range rows {
		detectedAt := rows[i].DetectedAt
		alarmSentAt := rows[i].AlarmSentAt
		inputs = append(inputs, normalizeObservationPostComparisonInput(
			kind,
			rows[i].PostID,
			rows[i].ContentID,
			rows[i].ChannelID,
			rows[i].ActualPublishedAt,
			&detectedAt,
			&alarmSentAt,
		))
	}
	return inputs
}

func (input *ObservationPostComparisonInput) ToObservationAlarmSentHistoryRow() ObservationAlarmSentHistoryRow {
	if input == nil {
		return ObservationAlarmSentHistoryRow{}
	}
	return ObservationAlarmSentHistoryRow{
		PostID:            strings.TrimSpace(input.CanonicalPostID),
		ContentID:         strings.TrimSpace(input.ContentID),
		ChannelID:         strings.TrimSpace(input.ChannelID),
		ActualPublishedAt: cloneObservationComparisonTime(input.ActualPublishedAt),
		DetectedAt:        observationComparisonTimeValue(input.DetectedAt),
		AlarmSentAt:       observationComparisonTimeValue(input.AlarmSentAt),
	}
}

func normalizeObservationPostComparisonInput(
	kind domain.OutboxKind,
	postID string,
	contentID string,
	channelID string,
	actualPublishedAt *time.Time,
	detectedAt *time.Time,
	alarmSentAt *time.Time,
) ObservationPostComparisonInput {
	return ObservationPostComparisonInput{
		Kind:              kind,
		AlarmType:         kind.ToAlarmType(),
		CanonicalPostID:   normalizeObservationComparisonCanonicalPostID(kind, postID, contentID),
		ContentID:         strings.TrimSpace(contentID),
		ChannelID:         strings.TrimSpace(channelID),
		TitleHint:         strings.TrimSpace(""),
		ActualPublishedAt: cloneObservationComparisonTime(actualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(detectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(alarmSentAt),
	}
}

func normalizeObservationComparisonCanonicalPostID(kind domain.OutboxKind, candidates ...string) string {
	for _, candidate := range candidates {
		normalized, err := ytcontentid.ForOutboxKind(kind, candidate)
		if err == nil && strings.TrimSpace(normalized) != "" {
			return strings.TrimSpace(normalized)
		}
	}
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneObservationComparisonTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func observationComparisonTimeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

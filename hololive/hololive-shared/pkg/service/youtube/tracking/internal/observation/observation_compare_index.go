package observation

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type observationPostComparisonKey struct {
	kind            domain.OutboxKind
	channelID       string
	canonicalPostID string
}

type observationPostComparisonAccumulator struct {
	representative ObservationPostComparisonInput
	count          int
}

func indexObservationPostComparisonInputs(
	inputs []ObservationPostComparisonInput,
) (result1 map[observationPostComparisonKey]*observationPostComparisonAccumulator, result2 []observationPostComparisonKey, result3 int) {
	index := make(map[observationPostComparisonKey]*observationPostComparisonAccumulator, len(inputs))
	keys := make([]observationPostComparisonKey, 0, len(inputs))
	duplicateInputCount := 0

	for i := range inputs {
		normalized := normalizeObservationPostComparisonComparableInput(&inputs[i])
		key := buildObservationPostComparisonKey(&normalized, i)
		if accumulator, ok := index[key]; ok {
			accumulator.representative = mergeObservationPostComparisonInputs(&accumulator.representative, &normalized)
			accumulator.count++
			duplicateInputCount++
			continue
		}

		index[key] = &observationPostComparisonAccumulator{
			representative: normalized,
			count:          1,
		}
		keys = append(keys, key)
	}

	sort.SliceStable(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		if keys[i].channelID != keys[j].channelID {
			return keys[i].channelID < keys[j].channelID
		}
		return keys[i].canonicalPostID < keys[j].canonicalPostID
	})

	return index, keys, duplicateInputCount
}

func normalizeObservationPostComparisonComparableInput(input *ObservationPostComparisonInput) ObservationPostComparisonInput {
	normalizedKind := input.Kind
	normalizedAlarmType := normalizedKind.ToAlarmType()
	if normalizedKind == "" && input.AlarmType.IsValid() {
		normalizedAlarmType = input.AlarmType
	}

	canonicalPostID := normalizeObservationComparisonCanonicalPostID(
		normalizedKind,
		input.CanonicalPostID,
		input.ContentID,
	)
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.CanonicalPostID)
	}
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.ContentID)
	}

	return ObservationPostComparisonInput{
		Kind:              normalizedKind,
		AlarmType:         normalizedAlarmType,
		CanonicalPostID:   canonicalPostID,
		ContentID:         strings.TrimSpace(input.ContentID),
		ChannelID:         strings.TrimSpace(input.ChannelID),
		TitleHint:         observationComparisonNormalizeTitleHint(input.TitleHint),
		ActualPublishedAt: cloneObservationComparisonTime(input.ActualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(input.DetectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(input.AlarmSentAt),
	}
}

func buildObservationPostComparisonKey(
	input *ObservationPostComparisonInput,
	index int,
) observationPostComparisonKey {
	canonicalPostID := strings.TrimSpace(input.CanonicalPostID)
	if canonicalPostID == "" {
		canonicalPostID = strings.TrimSpace(input.ContentID)
	}
	if canonicalPostID == "" {
		canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.DetectedAt).Format(time.RFC3339Nano)
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.AlarmSentAt).Format(time.RFC3339Nano)
		}
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":" + timeValueForObservationPostComparison(input.ActualPublishedAt).Format(time.RFC3339Nano)
		}
		if canonicalPostID == "__missing_post_id__::0001-01-01T00:00:00Z" {
			canonicalPostID = "__missing_post_id__:" + strings.TrimSpace(input.ChannelID) + ":idx:" + time.Duration(index).String()
		}
	}

	return observationPostComparisonKey{
		kind:            input.Kind,
		channelID:       strings.TrimSpace(input.ChannelID),
		canonicalPostID: canonicalPostID,
	}
}

func mergeObservationPostComparisonInputs(
	left *ObservationPostComparisonInput,
	right *ObservationPostComparisonInput,
) ObservationPostComparisonInput {
	merged := left
	merged.Kind = firstNonZeroObservationPostComparisonValue(merged.Kind, right.Kind)
	merged.AlarmType = firstNonZeroObservationPostComparisonValue(merged.AlarmType, right.AlarmType)
	merged.CanonicalPostID = firstNonBlankObservationPostComparisonString(merged.CanonicalPostID, right.CanonicalPostID)
	merged.ContentID = firstNonBlankObservationPostComparisonString(merged.ContentID, right.ContentID)
	merged.ChannelID = firstNonBlankObservationPostComparisonString(merged.ChannelID, right.ChannelID)
	merged.TitleHint = firstNonBlankObservationPostComparisonTitleHint(merged.TitleHint, right.TitleHint)
	merged.ActualPublishedAt = earliestObservationPostComparisonTime(merged.ActualPublishedAt, right.ActualPublishedAt)
	merged.DetectedAt = earliestObservationPostComparisonTime(merged.DetectedAt, right.DetectedAt)
	merged.AlarmSentAt = earliestObservationPostComparisonTime(merged.AlarmSentAt, right.AlarmSentAt)
	return *merged
}

func firstNonZeroObservationPostComparisonValue[T comparable](left, right T) T {
	var zero T
	if left == zero && right != zero {
		return right
	}
	return left
}

func firstNonBlankObservationPostComparisonString(left, right string) string {
	if strings.TrimSpace(left) != "" || strings.TrimSpace(right) == "" {
		return left
	}
	return strings.TrimSpace(right)
}

func firstNonBlankObservationPostComparisonTitleHint(left, right string) string {
	if strings.TrimSpace(left) != "" || strings.TrimSpace(right) == "" {
		return left
	}
	return observationComparisonNormalizeTitleHint(right)
}

func earliestObservationPostComparisonTime(left, right *time.Time) *time.Time {
	if left == nil {
		return cloneObservationComparisonTime(right)
	}
	if right == nil {
		return cloneObservationComparisonTime(left)
	}
	if right.Before(*left) {
		return cloneObservationComparisonTime(right)
	}
	return cloneObservationComparisonTime(left)
}

func buildObservationPostComparisonRow(
	baseline *observationPostComparisonAccumulator,
	sent *observationPostComparisonAccumulator,
) ObservationPostComparisonRow {
	var merged ObservationPostComparisonInput
	if baseline != nil {
		merged = baseline.representative
	}
	if sent != nil {
		merged = mergeObservationPostComparisonInputs(&merged, &sent.representative)
	}

	row := ObservationPostComparisonRow{
		Kind:              merged.Kind,
		AlarmType:         merged.AlarmType,
		ChannelID:         strings.TrimSpace(merged.ChannelID),
		CanonicalPostID:   strings.TrimSpace(merged.CanonicalPostID),
		ContentID:         strings.TrimSpace(merged.ContentID),
		TitleHint:         observationComparisonNormalizeTitleHint(merged.TitleHint),
		ActualPublishedAt: cloneObservationComparisonTime(merged.ActualPublishedAt),
		DetectedAt:        cloneObservationComparisonTime(merged.DetectedAt),
		AlarmSentAt:       cloneObservationComparisonTime(merged.AlarmSentAt),
	}
	if baseline != nil {
		row.BaselineCount = baseline.count
	}
	if sent != nil {
		row.SentCount = sent.count
	}
	return row
}

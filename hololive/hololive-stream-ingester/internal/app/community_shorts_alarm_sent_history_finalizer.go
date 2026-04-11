package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type observationAlarmSentHistoryFinalizationResult struct {
	Rows              []trackingrepo.ObservationAlarmSentHistoryRow
	CollectedRowCount int
	DuplicateRowCount int
}

func finalizeCommunityAlarmSentHistoryRows(rows []trackingrepo.CommunityAlarmSentHistoryRow) observationAlarmSentHistoryFinalizationResult {
	return finalizeObservationAlarmSentHistoryRows(domain.OutboxKindCommunityPost, rows)
}

func finalizeShortsAlarmSentHistoryRows(rows []trackingrepo.ShortsAlarmSentHistoryRow) observationAlarmSentHistoryFinalizationResult {
	return finalizeObservationAlarmSentHistoryRows(domain.OutboxKindNewShort, rows)
}

func finalizeObservationAlarmSentHistoryRows(
	kind domain.OutboxKind,
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
) observationAlarmSentHistoryFinalizationResult {
	if len(rows) == 0 {
		return observationAlarmSentHistoryFinalizationResult{}
	}

	inputs := trackingrepo.BuildObservationPostComparisonInputsFromSentHistories(kind, rows)
	rowsByPostID := make(map[string]trackingrepo.ObservationPostComparisonInput, len(inputs))
	orderedKeys := make([]string, 0, len(inputs))
	duplicateRowCount := 0

	for i := range inputs {
		row := inputs[i]
		key := strings.TrimSpace(row.CanonicalPostID)
		if key == "" {
			key = buildObservationAlarmSentHistoryFallbackKey(row, i)
		}

		if existing, ok := rowsByPostID[key]; ok {
			rowsByPostID[key] = mergeObservationAlarmSentHistoryInputs(existing, row)
			duplicateRowCount++
			continue
		}

		rowsByPostID[key] = row
		orderedKeys = append(orderedKeys, key)
	}

	finalInputs := make([]trackingrepo.ObservationPostComparisonInput, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		finalInputs = append(finalInputs, rowsByPostID[key])
	}

	sort.SliceStable(finalInputs, func(i, j int) bool {
		leftAlarmSentAt := observationAlarmSentHistoryTimeValue(finalInputs[i].AlarmSentAt)
		rightAlarmSentAt := observationAlarmSentHistoryTimeValue(finalInputs[j].AlarmSentAt)
		if !leftAlarmSentAt.Equal(rightAlarmSentAt) {
			return leftAlarmSentAt.Before(rightAlarmSentAt)
		}
		if finalInputs[i].CanonicalPostID != finalInputs[j].CanonicalPostID {
			return finalInputs[i].CanonicalPostID < finalInputs[j].CanonicalPostID
		}
		return finalInputs[i].ContentID < finalInputs[j].ContentID
	})

	finalRows := make([]trackingrepo.ObservationAlarmSentHistoryRow, 0, len(finalInputs))
	for i := range finalInputs {
		finalRows = append(finalRows, finalInputs[i].ToObservationAlarmSentHistoryRow())
	}

	return observationAlarmSentHistoryFinalizationResult{
		Rows:              finalRows,
		CollectedRowCount: len(rows),
		DuplicateRowCount: duplicateRowCount,
	}
}

func mergeObservationAlarmSentHistoryInputs(
	existing trackingrepo.ObservationPostComparisonInput,
	next trackingrepo.ObservationPostComparisonInput,
) trackingrepo.ObservationPostComparisonInput {
	merged := existing
	if merged.Kind == "" && next.Kind != "" {
		merged.Kind = next.Kind
	}
	if merged.AlarmType == "" && next.AlarmType != "" {
		merged.AlarmType = next.AlarmType
	}
	if strings.TrimSpace(merged.CanonicalPostID) == "" && strings.TrimSpace(next.CanonicalPostID) != "" {
		merged.CanonicalPostID = next.CanonicalPostID
	}
	if strings.TrimSpace(merged.ContentID) == "" && strings.TrimSpace(next.ContentID) != "" {
		merged.ContentID = next.ContentID
	}
	if strings.TrimSpace(merged.ChannelID) == "" && strings.TrimSpace(next.ChannelID) != "" {
		merged.ChannelID = next.ChannelID
	}
	merged.ActualPublishedAt = earliestObservationAlarmSentHistoryTime(merged.ActualPublishedAt, next.ActualPublishedAt)
	merged.DetectedAt = earliestObservationAlarmSentHistoryTime(merged.DetectedAt, next.DetectedAt)
	merged.AlarmSentAt = earliestObservationAlarmSentHistoryTime(merged.AlarmSentAt, next.AlarmSentAt)
	return merged
}

func earliestObservationAlarmSentHistoryTime(left *time.Time, right *time.Time) *time.Time {
	if left == nil {
		return cloneCommunityShortsSendCountTime(right)
	}
	if right == nil {
		return cloneCommunityShortsSendCountTime(left)
	}
	if right.Before(*left) {
		return cloneCommunityShortsSendCountTime(right)
	}
	return cloneCommunityShortsSendCountTime(left)
}

func buildObservationAlarmSentHistoryFallbackKey(row trackingrepo.ObservationPostComparisonInput, index int) string {
	return fmt.Sprintf("__row__:%d:%s:%s", index, strings.TrimSpace(row.ChannelID), observationAlarmSentHistoryTimeValue(row.AlarmSentAt).Format(time.RFC3339Nano))
}

func observationAlarmSentHistoryTimeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

package alarmhistory

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type finalizationResult struct {
	Rows              []trackingrepo.ObservationAlarmSentHistoryRow
	CollectedRowCount int
	DuplicateRowCount int
}

func finalizeCommunityAlarmSentHistoryRows(rows []trackingrepo.CommunityAlarmSentHistoryRow) finalizationResult {
	return finalizeAlarmSentHistoryRows(domain.OutboxKindCommunityPost, rows)
}

func finalizeShortsAlarmSentHistoryRows(rows []trackingrepo.ShortsAlarmSentHistoryRow) finalizationResult {
	return finalizeAlarmSentHistoryRows(domain.OutboxKindNewShort, rows)
}

func finalizeAlarmSentHistoryRows(
	kind domain.OutboxKind,
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
) finalizationResult {
	if len(rows) == 0 {
		return finalizationResult{}
	}

	inputs := trackingrepo.BuildObservationPostComparisonInputsFromSentHistories(kind, rows)
	finalInputs, duplicateRowCount := finalizeInputs(inputs)

	return finalizationResult{
		Rows:              buildFinalizedRows(finalInputs),
		CollectedRowCount: len(rows),
		DuplicateRowCount: duplicateRowCount,
	}
}

func finalizeInputs(
	inputs []trackingrepo.ObservationPostComparisonInput,
) (finalInputs []trackingrepo.ObservationPostComparisonInput, duplicateRowCount int) {
	rowsByPostID := make(map[string]trackingrepo.ObservationPostComparisonInput, len(inputs))
	orderedKeys := make([]string, 0, len(inputs))

	for i := range inputs {
		row := &inputs[i]
		key := strings.TrimSpace(row.CanonicalPostID)
		if key == "" {
			key = buildFallbackKey(row, i)
		}

		if existing, ok := rowsByPostID[key]; ok {
			rowsByPostID[key] = mergeInputs(&existing, row)
			duplicateRowCount++
			continue
		}

		rowsByPostID[key] = *row
		orderedKeys = append(orderedKeys, key)
	}

	finalInputs = orderedInputs(rowsByPostID, orderedKeys)
	sortInputs(finalInputs)
	return finalInputs, duplicateRowCount
}

func sortInputs(finalInputs []trackingrepo.ObservationPostComparisonInput) {
	sort.SliceStable(finalInputs, func(i, j int) bool {
		leftAlarmSentAt := timeValue(finalInputs[i].AlarmSentAt)
		rightAlarmSentAt := timeValue(finalInputs[j].AlarmSentAt)
		if !leftAlarmSentAt.Equal(rightAlarmSentAt) {
			return leftAlarmSentAt.Before(rightAlarmSentAt)
		}
		if finalInputs[i].CanonicalPostID != finalInputs[j].CanonicalPostID {
			return finalInputs[i].CanonicalPostID < finalInputs[j].CanonicalPostID
		}
		return finalInputs[i].ContentID < finalInputs[j].ContentID
	})
}

func orderedInputs(
	rowsByPostID map[string]trackingrepo.ObservationPostComparisonInput,
	orderedKeys []string,
) []trackingrepo.ObservationPostComparisonInput {
	finalInputs := make([]trackingrepo.ObservationPostComparisonInput, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		finalInputs = append(finalInputs, rowsByPostID[key])
	}
	return finalInputs
}

func buildFinalizedRows(
	finalInputs []trackingrepo.ObservationPostComparisonInput,
) []trackingrepo.ObservationAlarmSentHistoryRow {
	finalRows := make([]trackingrepo.ObservationAlarmSentHistoryRow, 0, len(finalInputs))
	for i := range finalInputs {
		finalRows = append(finalRows, finalInputs[i].ToObservationAlarmSentHistoryRow())
	}
	return finalRows
}

func mergeInputs(
	existing *trackingrepo.ObservationPostComparisonInput,
	next *trackingrepo.ObservationPostComparisonInput,
) trackingrepo.ObservationPostComparisonInput {
	if existing == nil {
		if next == nil {
			return trackingrepo.ObservationPostComparisonInput{}
		}
		return *next
	}
	if next == nil {
		return *existing
	}
	merged := *existing
	if merged.Kind == "" && next.Kind != "" {
		merged.Kind = next.Kind
	}
	if merged.AlarmType == "" && next.AlarmType != "" {
		merged.AlarmType = next.AlarmType
	}
	merged.CanonicalPostID = mergeString(merged.CanonicalPostID, next.CanonicalPostID)
	merged.ContentID = mergeString(merged.ContentID, next.ContentID)
	merged.ChannelID = mergeString(merged.ChannelID, next.ChannelID)
	merged.ActualPublishedAt = earliestTime(merged.ActualPublishedAt, next.ActualPublishedAt)
	merged.DetectedAt = earliestTime(merged.DetectedAt, next.DetectedAt)
	merged.AlarmSentAt = earliestTime(merged.AlarmSentAt, next.AlarmSentAt)
	return merged
}

func mergeString(existing, next string) string {
	if strings.TrimSpace(existing) == "" && strings.TrimSpace(next) != "" {
		return next
	}
	return existing
}

func earliestTime(left, right *time.Time) *time.Time {
	if left == nil {
		return shared.CloneSendCountTime(right)
	}
	if right == nil {
		return shared.CloneSendCountTime(left)
	}
	if right.Before(*left) {
		return shared.CloneSendCountTime(right)
	}
	return shared.CloneSendCountTime(left)
}

func buildFallbackKey(row *trackingrepo.ObservationPostComparisonInput, index int) string {
	if row == nil {
		return fmt.Sprintf("__row__:%d", index)
	}
	return fmt.Sprintf("__row__:%d:%s:%s", index, strings.TrimSpace(row.ChannelID), timeValue(row.AlarmSentAt).Format(time.RFC3339Nano))
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

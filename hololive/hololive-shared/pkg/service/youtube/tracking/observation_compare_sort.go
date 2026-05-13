package tracking

import (
	"cmp"
	"sort"
	"strings"
)

type observationPostComparisonRowComparator func(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int

var observationPostComparisonRowComparators = []observationPostComparisonRowComparator{
	compareObservationPostComparisonRowKind,
	compareObservationPostComparisonRowChannelID,
	compareObservationPostComparisonRowCanonicalPostID,
	compareObservationPostComparisonRowDetectedAt,
	compareObservationPostComparisonRowAlarmSentAt,
	compareObservationPostComparisonRowContentID,
	compareObservationPostComparisonRowTitleHint,
}

func sortObservationIdentifierMismatchCandidates(candidates []ObservationIdentifierMismatchCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		if candidates[i].ChannelID != candidates[j].ChannelID {
			return candidates[i].ChannelID < candidates[j].ChannelID
		}
		leftPublishedAt := timeValueForObservationPostComparison(candidates[i].MatchPublishedAt)
		rightPublishedAt := timeValueForObservationPostComparison(candidates[j].MatchPublishedAt)
		if !leftPublishedAt.Equal(rightPublishedAt) {
			return leftPublishedAt.Before(rightPublishedAt)
		}
		if candidates[i].MatchTitleHint != candidates[j].MatchTitleHint {
			return candidates[i].MatchTitleHint < candidates[j].MatchTitleHint
		}
		return len(candidates[i].BaselineRows) < len(candidates[j].BaselineRows)
	})
}

func sortObservationPostComparisonRows(rows []ObservationPostComparisonRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return compareObservationPostComparisonRows(rows[i], rows[j]) < 0
	})
}

func compareObservationPostComparisonRows(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	for _, comparator := range observationPostComparisonRowComparators {
		if result := comparator(left, right); result != 0 {
			return result
		}
	}
	return 0
}

func compareObservationPostComparisonRowKind(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return cmp.Compare(left.Kind, right.Kind)
}

func compareObservationPostComparisonRowChannelID(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return cmp.Compare(left.ChannelID, right.ChannelID)
}

func compareObservationPostComparisonRowCanonicalPostID(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return cmp.Compare(left.CanonicalPostID, right.CanonicalPostID)
}

func compareObservationPostComparisonRowDetectedAt(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return timeValueForObservationPostComparison(left.DetectedAt).Compare(timeValueForObservationPostComparison(right.DetectedAt))
}

func compareObservationPostComparisonRowAlarmSentAt(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return timeValueForObservationPostComparison(left.AlarmSentAt).Compare(timeValueForObservationPostComparison(right.AlarmSentAt))
}

func compareObservationPostComparisonRowContentID(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return cmp.Compare(strings.TrimSpace(left.ContentID), strings.TrimSpace(right.ContentID))
}

func compareObservationPostComparisonRowTitleHint(left ObservationPostComparisonRow, right ObservationPostComparisonRow) int {
	return cmp.Compare(observationComparisonTitleHintKey(left.TitleHint), observationComparisonTitleHintKey(right.TitleHint))
}

package tracking

import (
	"sort"
	"strings"
)

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
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		if rows[i].CanonicalPostID != rows[j].CanonicalPostID {
			return rows[i].CanonicalPostID < rows[j].CanonicalPostID
		}
		leftDetectedAt := timeValueForObservationPostComparison(rows[i].DetectedAt)
		rightDetectedAt := timeValueForObservationPostComparison(rows[j].DetectedAt)
		if !leftDetectedAt.Equal(rightDetectedAt) {
			return leftDetectedAt.Before(rightDetectedAt)
		}
		leftAlarmSentAt := timeValueForObservationPostComparison(rows[i].AlarmSentAt)
		rightAlarmSentAt := timeValueForObservationPostComparison(rows[j].AlarmSentAt)
		if !leftAlarmSentAt.Equal(rightAlarmSentAt) {
			return leftAlarmSentAt.Before(rightAlarmSentAt)
		}
		if strings.TrimSpace(rows[i].ContentID) != strings.TrimSpace(rows[j].ContentID) {
			return strings.TrimSpace(rows[i].ContentID) < strings.TrimSpace(rows[j].ContentID)
		}
		return observationComparisonTitleHintKey(rows[i].TitleHint) < observationComparisonTitleHintKey(rows[j].TitleHint)
	})
}

package reports

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

type communityShortsAlarmSentHistoryDatasetMissingAlarmSummary struct {
	SendStatePostCount        int
	MissingAlarmPostCount     int
	MissingSendStatePostCount int
	AttemptedMissingPostCount int
	NotSentMissingPostCount   int
}

func attachCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
	report CommunityShortsAlarmSentHistoryDatasetReport,
	sendStateRows []outbox.PostSendCount,
) CommunityShortsAlarmSentHistoryDatasetReport {
	sendStateReport := BuildCommunityShortsSendStateReport(
		sendStateRows,
		CommunityShortsSendStateQuery{
			ObservationRuntimeName:      report.Query.ObservationRuntimeName,
			ObservationBigBangCutoverAt: report.Query.ObservationBigBangCutoverAt,
			WindowStart:                 report.Query.WindowStart,
			WindowEnd:                   report.Query.WindowEnd,
			Finalized:                   true,
		},
		report.GeneratedAt,
	)
	missingRows, missingSummary := buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
		report.ReferenceRows,
		sendStateReport.Rows,
	)
	report.Summary.SendStatePostCount = missingSummary.SendStatePostCount
	report.Summary.MissingAlarmPostCount = missingSummary.MissingAlarmPostCount
	report.Summary.MissingSendStatePostCount = missingSummary.MissingSendStatePostCount
	report.Summary.AttemptedMissingPostCount = missingSummary.AttemptedMissingPostCount
	report.Summary.NotSentMissingPostCount = missingSummary.NotSentMissingPostCount
	report.MissingAlarmRows = missingRows
	report.Results = buildCommunityShortsAlarmSentHistoryDatasetResults(
		report.Rows,
		report.VerificationRows,
		report.ReferenceRows,
		report.MissingAlarmRows,
		report.Summary,
		true,
	)
	return report
}

func buildCommunityShortsAlarmSentHistoryDatasetMissingAlarmRows(
	referenceRows []CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	sendStateRows []CommunityShortsSendStateRow,
) ([]CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, communityShortsAlarmSentHistoryDatasetMissingAlarmSummary) {
	summary := communityShortsAlarmSentHistoryDatasetMissingAlarmSummary{
		SendStatePostCount: len(sendStateRows),
	}
	if len(referenceRows) == 0 {
		return nil, summary
	}

	sendStateByPostKey := buildCommunityShortsMissingAlarmSendStateIndex(sendStateRows)
	rows := make([]CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, 0, len(referenceRows))
	for i := range referenceRows {
		missingRow, ok := buildCommunityShortsMissingAlarmRow(referenceRows[i], sendStateByPostKey, &summary)
		if !ok {
			continue
		}
		rows = append(rows, missingRow)
	}

	sortCommunityShortsMissingAlarmRows(rows)

	return rows, summary
}

func buildCommunityShortsMissingAlarmSendStateIndex(sendStateRows []CommunityShortsSendStateRow) map[string]CommunityShortsSendStateRow {
	sendStateByPostKey := make(map[string]CommunityShortsSendStateRow, len(sendStateRows))
	for i := range sendStateRows {
		upsertCommunityShortsMissingAlarmSendState(sendStateByPostKey, sendStateRows[i])
	}
	return sendStateByPostKey
}

func upsertCommunityShortsMissingAlarmSendState(
	sendStateByPostKey map[string]CommunityShortsSendStateRow,
	row CommunityShortsSendStateRow,
) {
	postKey := missingAlarmSendStatePostKey(row)
	if postKey == "" {
		return
	}
	if existing, ok := sendStateByPostKey[postKey]; ok {
		sendStateByPostKey[postKey] = mergeCommunityShortsAlarmSentHistoryDatasetMissingAlarmStateRow(existing, row)
		return
	}
	sendStateByPostKey[postKey] = row
}

func missingAlarmSendStatePostKey(row CommunityShortsSendStateRow) string {
	postKey := strings.TrimSpace(row.PostKey)
	if postKey != "" {
		return postKey
	}
	return buildCommunityShortsObservationPostKey(row.ReportAlarmType, row.ReportChannelID, row.ReportPostID)
}

func buildCommunityShortsMissingAlarmRow(
	referenceRow CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	sendStateByPostKey map[string]CommunityShortsSendStateRow,
	summary *communityShortsAlarmSentHistoryDatasetMissingAlarmSummary,
) (CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, bool) {
	postKey := buildCommunityShortsObservationPostKey(referenceRow.AlarmType, referenceRow.ChannelID, referenceRow.PostID)
	stateRow, ok := sendStateByPostKey[postKey]
	if ok && stateRow.SendState == CommunityShortsPerPostSendStateSent {
		return CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow{}, false
	}

	missingRow := newCommunityShortsMissingAlarmRow(referenceRow, postKey)
	applyCommunityShortsMissingAlarmReason(&missingRow, stateRow, ok, summary)
	summary.MissingAlarmPostCount++
	return missingRow, true
}

func newCommunityShortsMissingAlarmRow(
	referenceRow CommunityShortsAlarmSentHistoryDatasetReferenceRow,
	postKey string,
) CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow {
	return CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow{
		AlarmType:           referenceRow.AlarmType,
		ChannelID:           referenceRow.ChannelID,
		ChannelPostKey:      referenceRow.ChannelPostKey,
		PostKey:             postKey,
		PostID:              referenceRow.PostID,
		ActualPublishedAt:   cloneCommunityShortsSendCountTime(referenceRow.ActualPublishedAt),
		DetectedAt:          cloneCommunityShortsSendCountTime(referenceRow.DetectedAt),
		VerificationVerdict: referenceRow.VerificationVerdict,
		VerificationReason:  referenceRow.VerificationReason,
		RelatedSentPostIDs:  cloneCommunityShortsAlarmSentHistoryDatasetStrings(referenceRow.RelatedSentPostIDs),
	}
}

func applyCommunityShortsMissingAlarmReason(
	missingRow *CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
	stateRow CommunityShortsSendStateRow,
	hasState bool,
	summary *communityShortsAlarmSentHistoryDatasetMissingAlarmSummary,
) {
	switch {
	case !hasState:
		summary.MissingSendStatePostCount++
		missingRow.MissingReason = CommunityShortsMissingAlarmReasonSendStateMissing
	case stateRow.SendState == CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
		summary.AttemptedMissingPostCount++
		fillCommunityShortsMissingAlarmState(missingRow, stateRow, CommunityShortsMissingAlarmReasonAttempted)
	default:
		summary.NotSentMissingPostCount++
		fillCommunityShortsMissingAlarmState(missingRow, stateRow, CommunityShortsMissingAlarmReasonNotSent)
	}
}

func fillCommunityShortsMissingAlarmState(
	missingRow *CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
	stateRow CommunityShortsSendStateRow,
	reason CommunityShortsMissingAlarmReason,
) {
	missingRow.MissingReason = reason
	missingRow.SendState = stateRow.SendState
	missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
	missingRow.StateDetectedAt = cloneCommunityShortsSendCountTime(stateRow.ReportDetectedAt)
	missingRow.StateAlarmSentAt = cloneCommunityShortsSendCountTime(stateRow.ReportAlarmSentAt)
}

func sortCommunityShortsMissingAlarmRows(rows []CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return lessCommunityShortsMissingAlarmRow(rows[i], rows[j])
	})
}

func lessCommunityShortsMissingAlarmRow(
	leftRow CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
	rightRow CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
) bool {
	left := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(leftRow)
	right := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(rightRow)
	if !left.Equal(right) {
		return left.Before(right)
	}
	if leftRow.AlarmType != rightRow.AlarmType {
		return leftRow.AlarmType < rightRow.AlarmType
	}
	if leftRow.ChannelID != rightRow.ChannelID {
		return leftRow.ChannelID < rightRow.ChannelID
	}
	return leftRow.PostID < rightRow.PostID
}

func mergeCommunityShortsAlarmSentHistoryDatasetMissingAlarmStateRow(
	current CommunityShortsSendStateRow,
	next CommunityShortsSendStateRow,
) CommunityShortsSendStateRow {
	if communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(next.SendState) > communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(current.SendState) {
		return next
	}
	return current
}

func communityShortsAlarmSentHistoryDatasetMissingAlarmStatePriority(state CommunityShortsPerPostSendState) int {
	switch state {
	case CommunityShortsPerPostSendStateSent:
		return 30
	case CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
		return 20
	case CommunityShortsPerPostSendStateNotSent:
		return 10
	default:
		return 0
	}
}

func communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(
	row CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow,
) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt, row.StateDetectedAt, row.StateAlarmSentAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

package alarmhistory

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendstate"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type missingAlarmSummary struct {
	SendStatePostCount        int
	MissingAlarmPostCount     int
	MissingSendStatePostCount int
	AttemptedMissingPostCount int
	NotSentMissingPostCount   int
}

func attachDatasetMissingAlarmRows(
	report DatasetReport,
	sendStateRows []outbox.PostSendCount,
) DatasetReport {
	sendStateReport := sendstate.Build(
		sendStateRows,
		sendstate.Query{
			ObservationRuntimeName:      report.Query.ObservationRuntimeName,
			ObservationBigBangCutoverAt: report.Query.ObservationBigBangCutoverAt,
			WindowStart:                 report.Query.WindowStart,
			WindowEnd:                   report.Query.WindowEnd,
			Finalized:                   true,
		},
		report.GeneratedAt,
	)
	missingRows, mSummary := buildMissingAlarmRows(report.ReferenceRows, sendStateReport.Rows)
	report.Summary.SendStatePostCount = mSummary.SendStatePostCount
	report.Summary.MissingAlarmPostCount = mSummary.MissingAlarmPostCount
	report.Summary.MissingSendStatePostCount = mSummary.MissingSendStatePostCount
	report.Summary.AttemptedMissingPostCount = mSummary.AttemptedMissingPostCount
	report.Summary.NotSentMissingPostCount = mSummary.NotSentMissingPostCount
	report.MissingAlarmRows = missingRows
	report.Results = buildDatasetResults(
		report.Rows,
		report.VerificationRows,
		report.ReferenceRows,
		report.MissingAlarmRows,
		report.Summary,
		true,
	)
	return report
}

func buildMissingAlarmRows(
	referenceRows []DatasetReferenceRow,
	sendStateRows []sendstate.Row,
) ([]DatasetMissingAlarmRow, missingAlarmSummary) {
	summary := missingAlarmSummary{
		SendStatePostCount: len(sendStateRows),
	}
	if len(referenceRows) == 0 {
		return nil, summary
	}

	sendStateByPostKey := buildMissingAlarmSendStateIndex(sendStateRows)
	rows := make([]DatasetMissingAlarmRow, 0, len(referenceRows))
	for i := range referenceRows {
		missingRow, ok := buildMissingAlarmRow(referenceRows[i], sendStateByPostKey, &summary)
		if !ok {
			continue
		}
		rows = append(rows, missingRow)
	}

	sortMissingAlarmRows(rows)

	return rows, summary
}

func buildMissingAlarmSendStateIndex(sendStateRows []sendstate.Row) map[string]sendstate.Row {
	sendStateByPostKey := make(map[string]sendstate.Row, len(sendStateRows))
	for i := range sendStateRows {
		upsertMissingAlarmSendState(sendStateByPostKey, sendStateRows[i])
	}
	return sendStateByPostKey
}

func upsertMissingAlarmSendState(
	sendStateByPostKey map[string]sendstate.Row,
	row sendstate.Row,
) {
	postKey := missingAlarmSendStatePostKey(row)
	if postKey == "" {
		return
	}
	if existing, ok := sendStateByPostKey[postKey]; ok {
		sendStateByPostKey[postKey] = mergeMissingAlarmStateRow(existing, row)
		return
	}
	sendStateByPostKey[postKey] = row
}

func missingAlarmSendStatePostKey(row sendstate.Row) string {
	postKey := strings.TrimSpace(row.PostKey)
	if postKey != "" {
		return postKey
	}
	return buildObservationPostKey(row.ReportAlarmType, row.ReportChannelID, row.ReportPostID)
}

func buildMissingAlarmRow(
	referenceRow DatasetReferenceRow,
	sendStateByPostKey map[string]sendstate.Row,
	summary *missingAlarmSummary,
) (DatasetMissingAlarmRow, bool) {
	postKey := buildObservationPostKey(referenceRow.AlarmType, referenceRow.ChannelID, referenceRow.PostID)
	stateRow, ok := sendStateByPostKey[postKey]
	if ok && stateRow.SendState == sendstate.PerPostStateSent {
		return DatasetMissingAlarmRow{}, false
	}

	missingRow := newMissingAlarmRow(referenceRow, postKey)
	applyMissingAlarmReason(&missingRow, stateRow, ok, summary)
	summary.MissingAlarmPostCount++
	return missingRow, true
}

func newMissingAlarmRow(
	referenceRow DatasetReferenceRow,
	postKey string,
) DatasetMissingAlarmRow {
	return DatasetMissingAlarmRow{
		AlarmType:           referenceRow.AlarmType,
		ChannelID:           referenceRow.ChannelID,
		ChannelPostKey:      referenceRow.ChannelPostKey,
		PostKey:             postKey,
		PostID:              referenceRow.PostID,
		ActualPublishedAt:   shared.CloneSendCountTime(referenceRow.ActualPublishedAt),
		DetectedAt:          shared.CloneSendCountTime(referenceRow.DetectedAt),
		VerificationVerdict: referenceRow.VerificationVerdict,
		VerificationReason:  referenceRow.VerificationReason,
		RelatedSentPostIDs:  cloneStrings(referenceRow.RelatedSentPostIDs),
	}
}

func applyMissingAlarmReason(
	missingRow *DatasetMissingAlarmRow,
	stateRow sendstate.Row,
	hasState bool,
	summary *missingAlarmSummary,
) {
	switch {
	case !hasState:
		summary.MissingSendStatePostCount++
		missingRow.MissingReason = MissingAlarmReasonSendStateMissing
	case stateRow.SendState == sendstate.PerPostStateAttemptedWithoutSuccess:
		summary.AttemptedMissingPostCount++
		fillMissingAlarmState(missingRow, stateRow, MissingAlarmReasonAttempted)
	default:
		summary.NotSentMissingPostCount++
		fillMissingAlarmState(missingRow, stateRow, MissingAlarmReasonNotSent)
	}
}

func fillMissingAlarmState(
	missingRow *DatasetMissingAlarmRow,
	stateRow sendstate.Row,
	reason MissingAlarmReason,
) {
	missingRow.MissingReason = reason
	missingRow.SendState = stateRow.SendState
	missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
	missingRow.StateDetectedAt = shared.CloneSendCountTime(stateRow.ReportDetectedAt)
	missingRow.StateAlarmSentAt = shared.CloneSendCountTime(stateRow.ReportAlarmSentAt)
}

func sortMissingAlarmRows(rows []DatasetMissingAlarmRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return lessMissingAlarmRow(rows[i], rows[j])
	})
}

func lessMissingAlarmRow(leftRow DatasetMissingAlarmRow, rightRow DatasetMissingAlarmRow) bool {
	left := missingAlarmSortTime(leftRow)
	right := missingAlarmSortTime(rightRow)
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

func mergeMissingAlarmStateRow(current sendstate.Row, next sendstate.Row) sendstate.Row {
	if missingAlarmStatePriority(next.SendState) > missingAlarmStatePriority(current.SendState) {
		return next
	}
	return current
}

func missingAlarmStatePriority(state sendstate.PerPostState) int {
	switch state {
	case sendstate.PerPostStateSent:
		return 30
	case sendstate.PerPostStateAttemptedWithoutSuccess:
		return 20
	case sendstate.PerPostStateNotSent:
		return 10
	default:
		return 0
	}
}

func missingAlarmSortTime(row DatasetMissingAlarmRow) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt, row.StateDetectedAt, row.StateAlarmSentAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

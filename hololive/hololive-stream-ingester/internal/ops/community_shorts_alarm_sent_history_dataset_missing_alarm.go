package ops

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

	sendStateByPostKey := make(map[string]CommunityShortsSendStateRow, len(sendStateRows))
	for i := range sendStateRows {
		row := sendStateRows[i]
		postKey := strings.TrimSpace(row.PostKey)
		if postKey == "" {
			postKey = buildCommunityShortsObservationPostKey(row.ReportAlarmType, row.ReportChannelID, row.ReportPostID)
		}
		if postKey == "" {
			continue
		}
		if existing, ok := sendStateByPostKey[postKey]; ok {
			sendStateByPostKey[postKey] = mergeCommunityShortsAlarmSentHistoryDatasetMissingAlarmStateRow(existing, row)
			continue
		}
		sendStateByPostKey[postKey] = row
	}

	rows := make([]CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow, 0, len(referenceRows))
	for i := range referenceRows {
		referenceRow := referenceRows[i]
		postKey := buildCommunityShortsObservationPostKey(referenceRow.AlarmType, referenceRow.ChannelID, referenceRow.PostID)
		stateRow, ok := sendStateByPostKey[postKey]
		if ok && stateRow.SendState == CommunityShortsPerPostSendStateSent {
			continue
		}

		missingRow := CommunityShortsAlarmSentHistoryDatasetMissingAlarmRow{
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

		switch {
		case !ok:
			summary.MissingSendStatePostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonSendStateMissing
		case stateRow.SendState == CommunityShortsPerPostSendStateAttemptedWithoutSuccess:
			summary.AttemptedMissingPostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonAttempted
			missingRow.SendState = stateRow.SendState
			missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
			missingRow.StateDetectedAt = cloneCommunityShortsSendCountTime(stateRow.ReportDetectedAt)
			missingRow.StateAlarmSentAt = cloneCommunityShortsSendCountTime(stateRow.ReportAlarmSentAt)
		default:
			summary.NotSentMissingPostCount++
			missingRow.MissingReason = CommunityShortsMissingAlarmReasonNotSent
			missingRow.SendState = stateRow.SendState
			missingRow.StateContentID = strings.TrimSpace(stateRow.ContentID)
			missingRow.StateDetectedAt = cloneCommunityShortsSendCountTime(stateRow.ReportDetectedAt)
			missingRow.StateAlarmSentAt = cloneCommunityShortsSendCountTime(stateRow.ReportAlarmSentAt)
		}

		summary.MissingAlarmPostCount++
		rows = append(rows, missingRow)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(rows[i])
		right := communityShortsAlarmSentHistoryDatasetMissingAlarmSortTime(rows[j])
		if !left.Equal(right) {
			return left.Before(right)
		}
		if rows[i].AlarmType != rows[j].AlarmType {
			return rows[i].AlarmType < rows[j].AlarmType
		}
		if rows[i].ChannelID != rows[j].ChannelID {
			return rows[i].ChannelID < rows[j].ChannelID
		}
		return rows[i].PostID < rows[j].PostID
	})

	return rows, summary
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

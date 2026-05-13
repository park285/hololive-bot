package ops

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func BuildCommunityShortsSendCountReport(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	generatedAt time.Time,
	since time.Time,
) CommunityShortsSendCountReport {
	query := CommunityShortsSendCountQuery{
		Mode:        communityShortsSendCountQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&generatedAt),
	}
	return BuildCommunityShortsSendCountReportWithQuery(sendCountRows, timelineRows, query, generatedAt)
}

func BuildCommunityShortsSendCountReportWithQuery(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query CommunityShortsSendCountQuery,
	generatedAt time.Time,
) CommunityShortsSendCountReport {
	generatedAt, query = resolveCommunityShortsSendCountReportInputs(generatedAt, query)

	timelineIndex := buildCommunityShortsDeliveryTimelineIndex(timelineRows)

	normalizedRows := make([]CommunityShortsSendCountRow, 0, len(sendCountRows))
	summary := CommunityShortsSendCountSummary{}
	for i := range sendCountRows {
		row := buildCommunityShortsSendCountRow(sendCountRows[i], timelineIndex)
		normalizedRows = append(normalizedRows, row)
		accumulateCommunityShortsSendCountSummary(&summary, row)
	}

	sortCommunityShortsSendCountRows(normalizedRows)
	return buildCommunityShortsSendCountReport(generatedAt, query, summary, normalizedRows)
}

func sortCommunityShortsSendCountRows(normalizedRows []CommunityShortsSendCountRow) {
	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := communityShortsSendCountSortTime(normalizedRows[i])
		right := communityShortsSendCountSortTime(normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		if normalizedRows[i].AlarmType != normalizedRows[j].AlarmType {
			return normalizedRows[i].AlarmType < normalizedRows[j].AlarmType
		}
		if normalizedRows[i].ChannelID != normalizedRows[j].ChannelID {
			return normalizedRows[i].ChannelID < normalizedRows[j].ChannelID
		}
		if normalizedRows[i].PostID != normalizedRows[j].PostID {
			return normalizedRows[i].PostID < normalizedRows[j].PostID
		}
		return normalizedRows[i].ContentID < normalizedRows[j].ContentID
	})
}

func buildCommunityShortsSendCountReport(
	generatedAt time.Time,
	query CommunityShortsSendCountQuery,
	summary CommunityShortsSendCountSummary,
	normalizedRows []CommunityShortsSendCountRow,
) CommunityShortsSendCountReport {
	windowStart := normalizeCommunityShortsSendCountTimePtrValue(query.WindowStart)
	windowEnd := normalizeCommunityShortsSendCountTimePtrValue(query.WindowEnd)
	return CommunityShortsSendCountReport{
		GeneratedAt:  generatedAt,
		Query:        query,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Summary:      summary,
		Verification: buildCommunityShortsSendCountVerification(summary),
		Rows:         normalizedRows,
	}
}

func resolveCommunityShortsSendCountReportInputs(
	generatedAt time.Time,
	query CommunityShortsSendCountQuery,
) (time.Time, CommunityShortsSendCountQuery) {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsSendCountQuery(query)
	if query.Mode == "" {
		query.Mode = communityShortsSendCountQueryModeRecent
	}
	if query.WindowEnd == nil {
		query.WindowEnd = cloneCommunityShortsSendCountTime(&generatedAt)
	}
	return generatedAt, query
}

func buildCommunityShortsSendCountRow(
	sendCountRow outbox.PostSendCount,
	timelineIndex map[communityShortsSendCountKey]outbox.PostDeliveryTimeline,
) CommunityShortsSendCountRow {
	row := CommunityShortsSendCountRow{PostSendCount: normalizeCommunityShortsPostSendCount(sendCountRow)}
	row.ReportAlarmType = row.AlarmType
	row.ReportChannelID = row.ChannelID
	row.ReportPostID = resolveCommunityShortsSendCountPostID(row)
	row.ReportActualPublishedAt = cloneCommunityShortsSendCountTime(row.ActualPublishedAt)
	row.ReportAlarmSentAt = resolveCommunityShortsSendCountAlarmSentAt(row)
	row.ReportDelaySeconds = buildCommunityShortsSendCountDelaySeconds(
		row.AlarmLatencyMillis,
		row.ReportActualPublishedAt,
		row.ReportAlarmSentAt,
	)
	applyCommunityShortsSendCountTimeline(&row, timelineIndex[buildCommunityShortsSendCountKey(row.ChannelID, row.AlarmType, row.ContentID)])
	return row
}

func applyCommunityShortsSendCountTimeline(
	row *CommunityShortsSendCountRow,
	timeline outbox.PostDeliveryTimeline,
) {
	if row == nil {
		return
	}
	row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(timeline.PublishToDetectMillis)
	row.DelaySource = timeline.DelaySource
	row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(timeline.QueueWaitMillis)
	row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(timeline.RetryAccumulationMillis)
	row.JobFailureDetected = timeline.JobFailureDetected
	row.InternalDelayCause = timeline.InternalDelayCause
	row.LatencyClassification = cloneCommunityShortsLatencyClassification(timeline.LatencyClassification)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
}

func accumulateCommunityShortsSendCountSummary(
	summary *CommunityShortsSendCountSummary,
	row CommunityShortsSendCountRow,
) {
	if summary == nil {
		return
	}
	summary.PostCount++
	accumulateCommunityShortsSendOutcome(summary, row)
	accumulateCommunityShortsDelaySource(summary, row.DelaySource)
	accumulateCommunityShortsInternalDelayCause(summary, row.InternalDelayCause)
}

func accumulateCommunityShortsSendOutcome(
	summary *CommunityShortsSendCountSummary,
	row CommunityShortsSendCountRow,
) {
	if row.SuccessSendCount > 0 {
		summary.SuccessfulPostCount++
	} else {
		summary.ZeroSuccessPostCount++
	}
	if row.DuplicateSuccessCount > 0 {
		summary.DuplicateSuccessPostCount++
	}
	if row.FailedAttemptCount > 0 {
		summary.FailedAttemptPostCount++
	}
	if row.OutboxCount == 0 {
		summary.OutboxMissingPostCount++
	}
}

func accumulateCommunityShortsDelaySource(summary *CommunityShortsSendCountSummary, delaySource outbox.PostDelaySource) {
	switch delaySource {
	case outbox.PostDelaySourceExternalCollection:
		summary.ExternalCollectionSourcePostCount++
	case outbox.PostDelaySourceInternalDelivery:
		summary.InternalDeliverySourcePostCount++
	case outbox.PostDelaySourceMixed:
		summary.MixedDelaySourcePostCount++
	}
}

func accumulateCommunityShortsInternalDelayCause(summary *CommunityShortsSendCountSummary, internalDelayCause outbox.PostInternalDelayCause) {
	switch internalDelayCause {
	case outbox.PostInternalDelayCauseQueueWait:
		summary.QueueWaitCausePostCount++
	case outbox.PostInternalDelayCauseRetryAccumulation:
		summary.RetryAccumulationCausePostCount++
	case outbox.PostInternalDelayCauseJobFailure:
		summary.JobFailureCausePostCount++
	}
}

func buildCommunityShortsDeliveryTimelineIndex(timelineRows []outbox.PostDeliveryTimeline) map[communityShortsSendCountKey]outbox.PostDeliveryTimeline {
	timelineIndex := make(map[communityShortsSendCountKey]outbox.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeCommunityShortsDeliveryTimeline(timelineRows[i])
		key := buildCommunityShortsSendCountKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		timelineIndex[key] = timeline
	}
	return timelineIndex
}

func buildCommunityShortsSendCountKey(channelID string, alarmType domain.AlarmType, contentID string) communityShortsSendCountKey {
	return communityShortsSendCountKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
		contentID: strings.TrimSpace(contentID),
	}
}

func normalizeCommunityShortsPostSendCount(row outbox.PostSendCount) outbox.PostSendCount {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.ActualPublishedAt = cloneCommunityShortsSendCountTime(row.ActualPublishedAt)
	row.DetectedAt = cloneCommunityShortsSendCountTime(row.DetectedAt)
	row.AlarmSentAt = cloneCommunityShortsSendCountTime(row.AlarmSentAt)
	row.FirstEventAt = cloneCommunityShortsSendCountTime(row.FirstEventAt)
	row.LastEventAt = cloneCommunityShortsSendCountTime(row.LastEventAt)
	row.FirstSuccessAt = cloneCommunityShortsSendCountTime(row.FirstSuccessAt)
	row.LastSuccessAt = cloneCommunityShortsSendCountTime(row.LastSuccessAt)
	return row
}

func normalizeCommunityShortsDeliveryTimeline(row outbox.PostDeliveryTimeline) outbox.PostDeliveryTimeline {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(row.PublishToDetectMillis)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(row.QueueWaitMillis)
	row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(row.RetryAccumulationMillis)
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	row.LatencyClassification = cloneCommunityShortsLatencyClassification(row.LatencyClassification)
	return row
}

func normalizeCommunityShortsSendCountQuery(query CommunityShortsSendCountQuery) CommunityShortsSendCountQuery {
	query.Mode = CommunityShortsSendCountQueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func communityShortsSendCountSortTime(row CommunityShortsSendCountRow) time.Time {
	for _, candidate := range []*time.Time{row.LastSuccessAt, row.LastEventAt, row.AlarmSentAt, row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func resolveCommunityShortsSendCountAlarmSentAt(row CommunityShortsSendCountRow) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return cloneCommunityShortsSendCountTime(candidate)
		}
	}
	return nil
}

func resolveCommunityShortsSendCountPostID(row CommunityShortsSendCountRow) string {
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func buildCommunityShortsSendCountDelaySeconds(
	latencyMillis *int64,
	actualPublishedAt *time.Time,
	alarmSentAt *time.Time,
) *float64 {
	if latencyMillis != nil {
		seconds := float64(*latencyMillis) / float64(time.Second/time.Millisecond)
		return &seconds
	}
	if actualPublishedAt == nil || alarmSentAt == nil {
		return nil
	}
	seconds := alarmSentAt.UTC().Sub(actualPublishedAt.UTC()).Seconds()
	return &seconds
}

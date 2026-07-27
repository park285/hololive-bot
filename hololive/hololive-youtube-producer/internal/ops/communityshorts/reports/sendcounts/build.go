package sendcounts

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	deliverytimeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/shared"
)

func Build(
	sendCountRows []analytics.PostSendCount,
	timelineRows []deliverytimeline.PostDeliveryTimeline,
	generatedAt time.Time,
	since time.Time,
) Report {
	query := Query{
		Mode:        QueryModeRecent,
		WindowStart: shared.CloneSendCountTime(&since),
		WindowEnd:   shared.CloneSendCountTime(&generatedAt),
	}
	return BuildWithQuery(sendCountRows, timelineRows, query, generatedAt)
}

func BuildWithQuery(
	sendCountRows []analytics.PostSendCount,
	timelineRows []deliverytimeline.PostDeliveryTimeline,
	query Query,
	generatedAt time.Time,
) Report {
	generatedAt, query = resolveReportInputs(generatedAt, query)

	timelineIndex := buildDeliveryTimelineIndex(timelineRows)

	normalizedRows := make([]Row, 0, len(sendCountRows))
	summary := Summary{}
	for i := range sendCountRows {
		row := buildRow(&sendCountRows[i], timelineIndex)
		normalizedRows = append(normalizedRows, row)
		accumulateSummary(&summary, &row)
	}

	sortRows(normalizedRows)
	return assembleReport(generatedAt, query, &summary, normalizedRows)
}

func sortRows(normalizedRows []Row) {
	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := rowSortTime(&normalizedRows[i])
		right := rowSortTime(&normalizedRows[j])
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

func assembleReport(
	generatedAt time.Time,
	query Query,
	summary *Summary,
	normalizedRows []Row,
) Report {
	if summary == nil {
		summary = &Summary{}
	}
	windowStart := shared.NormalizeSendCountTimePtrValue(query.WindowStart)
	windowEnd := shared.NormalizeSendCountTimePtrValue(query.WindowEnd)
	return Report{
		GeneratedAt:  generatedAt,
		Query:        query,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Summary:      *summary,
		Verification: buildVerification(summary),
		Rows:         normalizedRows,
	}
}

func resolveReportInputs(
	generatedAt time.Time,
	query Query,
) (time.Time, Query) {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeQuery(query)
	if query.Mode == "" {
		query.Mode = QueryModeRecent
	}
	if query.WindowEnd == nil {
		query.WindowEnd = shared.CloneSendCountTime(&generatedAt)
	}
	return generatedAt, query
}

func buildRow(
	sendCountRow *analytics.PostSendCount,
	timelineIndex map[sendCountKey]deliverytimeline.PostDeliveryTimeline,
) Row {
	row := Row{PostSendCount: normalizePostSendCount(sendCountRow)}
	row.ReportAlarmType = row.AlarmType
	row.ReportChannelID = row.ChannelID
	row.ReportPostID = resolvePostID(&row)
	row.ReportActualPublishedAt = shared.CloneSendCountTime(row.ActualPublishedAt)
	row.ReportAlarmSentAt = resolveAlarmSentAt(&row)
	row.ReportDelaySeconds = buildDelaySeconds(
		row.AlarmLatencyMillis,
		row.ReportActualPublishedAt,
		row.ReportAlarmSentAt,
	)
	timeline := timelineIndex[buildKey(row.ChannelID, row.AlarmType, row.ContentID)]
	applyTimeline(&row, &timeline)
	return row
}

func applyTimeline(
	row *Row,
	deliveryTimeline *deliverytimeline.PostDeliveryTimeline,
) {
	if row == nil || deliveryTimeline == nil {
		return
	}
	row.PublishToDetectMillis = shared.CloneSendCountInt64(deliveryTimeline.PublishToDetectMillis)
	row.DelaySource = deliveryTimeline.DelaySource
	row.QueueWaitMillis = shared.CloneSendCountInt64(deliveryTimeline.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(deliveryTimeline.RetryAccumulationMillis)
	row.JobFailureDetected = deliveryTimeline.JobFailureDetected
	row.InternalDelayCause = deliveryTimeline.InternalDelayCause
	row.LatencyClassification = shared.CloneLatencyClassification(&deliveryTimeline.LatencyClassification)
	if row.DelaySource == "" {
		row.DelaySource = deliverytimeline.PostDelaySourceNone
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = deliverytimeline.PostInternalDelayCauseNone
	}
}

func accumulateSummary(summary *Summary, row *Row) {
	if summary == nil {
		return
	}
	summary.PostCount++
	accumulateSendOutcome(summary, row)
	accumulateDelaySource(summary, row.DelaySource)
	accumulateInternalDelayCause(summary, row.InternalDelayCause)
}

func accumulateSendOutcome(summary *Summary, row *Row) {
	if row == nil {
		return
	}
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

func accumulateDelaySource(summary *Summary, delaySource deliverytimeline.PostDelaySource) {
	if delaySource == deliverytimeline.PostDelaySourceExternalCollection {
		summary.ExternalCollectionSourcePostCount++

		return
	}
	if delaySource == deliverytimeline.PostDelaySourceInternalDelivery {
		summary.InternalDeliverySourcePostCount++

		return
	}
	if delaySource == deliverytimeline.PostDelaySourceMixed {
		summary.MixedDelaySourcePostCount++
	}
}

func accumulateInternalDelayCause(summary *Summary, internalDelayCause deliverytimeline.PostInternalDelayCause) {
	if internalDelayCause == deliverytimeline.PostInternalDelayCauseQueueWait {
		summary.QueueWaitCausePostCount++

		return
	}
	if internalDelayCause == deliverytimeline.PostInternalDelayCauseRetryAccumulation {
		summary.RetryAccumulationCausePostCount++

		return
	}
	if internalDelayCause == deliverytimeline.PostInternalDelayCauseJobFailure {
		summary.JobFailureCausePostCount++
	}
}

func buildDeliveryTimelineIndex(timelineRows []deliverytimeline.PostDeliveryTimeline) map[sendCountKey]deliverytimeline.PostDeliveryTimeline {
	timelineIndex := make(map[sendCountKey]deliverytimeline.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeDeliveryTimeline(&timelineRows[i])
		key := buildKey(timeline.ChannelID, timeline.AlarmType, timeline.ContentID)
		if key.contentID == "" {
			continue
		}
		timelineIndex[key] = timeline
	}
	return timelineIndex
}

func buildKey(channelID string, alarmType domain.AlarmType, contentID string) sendCountKey {
	return sendCountKey{
		channelID: strings.TrimSpace(channelID),
		alarmType: alarmType,
		contentID: strings.TrimSpace(contentID),
	}
}

func normalizePostSendCount(row *analytics.PostSendCount) analytics.PostSendCount {
	if row == nil {
		return analytics.PostSendCount{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.ActualPublishedAt = shared.CloneSendCountTime(normalized.ActualPublishedAt)
	normalized.DetectedAt = shared.CloneSendCountTime(normalized.DetectedAt)
	normalized.AlarmSentAt = shared.CloneSendCountTime(normalized.AlarmSentAt)
	normalized.FirstEventAt = shared.CloneSendCountTime(normalized.FirstEventAt)
	normalized.LastEventAt = shared.CloneSendCountTime(normalized.LastEventAt)
	normalized.FirstSuccessAt = shared.CloneSendCountTime(normalized.FirstSuccessAt)
	normalized.LastSuccessAt = shared.CloneSendCountTime(normalized.LastSuccessAt)
	return normalized
}

func normalizeDeliveryTimeline(row *deliverytimeline.PostDeliveryTimeline) deliverytimeline.PostDeliveryTimeline {
	if row == nil {
		return deliverytimeline.PostDeliveryTimeline{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.PublishToDetectMillis = shared.CloneSendCountInt64(normalized.PublishToDetectMillis)
	if normalized.DelaySource == "" {
		normalized.DelaySource = deliverytimeline.PostDelaySourceNone
	}
	normalized.QueueWaitMillis = shared.CloneSendCountInt64(normalized.QueueWaitMillis)
	normalized.RetryAccumulationMillis = shared.CloneSendCountInt64(normalized.RetryAccumulationMillis)
	if normalized.InternalDelayCause == "" {
		normalized.InternalDelayCause = deliverytimeline.PostInternalDelayCauseNone
	}
	normalized.LatencyClassification = shared.CloneLatencyClassification(&normalized.LatencyClassification)
	return normalized
}

func normalizeQuery(query Query) Query {
	query.Mode = QueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	return query
}

func rowSortTime(row *Row) time.Time {
	if row == nil {
		return time.Time{}
	}
	for _, candidate := range []*time.Time{row.LastSuccessAt, row.LastEventAt, row.AlarmSentAt, row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func resolveAlarmSentAt(row *Row) *time.Time {
	if row == nil {
		return nil
	}
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return shared.CloneSendCountTime(candidate)
		}
	}
	return nil
}

func resolvePostID(row *Row) string {
	if row == nil {
		return ""
	}
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func buildDelaySeconds(
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

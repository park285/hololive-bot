package sendcounts

import (
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func Build(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
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
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query Query,
	generatedAt time.Time,
) Report {
	generatedAt, query = resolveReportInputs(generatedAt, query)

	timelineIndex := buildDeliveryTimelineIndex(timelineRows)

	normalizedRows := make([]Row, 0, len(sendCountRows))
	summary := Summary{}
	for i := range sendCountRows {
		row := buildRow(sendCountRows[i], timelineIndex)
		normalizedRows = append(normalizedRows, row)
		accumulateSummary(&summary, row)
	}

	sortRows(normalizedRows)
	return assembleReport(generatedAt, query, summary, normalizedRows)
}

func sortRows(normalizedRows []Row) {
	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := rowSortTime(normalizedRows[i])
		right := rowSortTime(normalizedRows[j])
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
	summary Summary,
	normalizedRows []Row,
) Report {
	windowStart := shared.NormalizeSendCountTimePtrValue(query.WindowStart)
	windowEnd := shared.NormalizeSendCountTimePtrValue(query.WindowEnd)
	return Report{
		GeneratedAt:  generatedAt,
		Query:        query,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		Summary:      summary,
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
	sendCountRow outbox.PostSendCount,
	timelineIndex map[sendCountKey]outbox.PostDeliveryTimeline,
) Row {
	row := Row{PostSendCount: normalizePostSendCount(sendCountRow)}
	row.ReportAlarmType = row.AlarmType
	row.ReportChannelID = row.ChannelID
	row.ReportPostID = resolvePostID(row)
	row.ReportActualPublishedAt = shared.CloneSendCountTime(row.ActualPublishedAt)
	row.ReportAlarmSentAt = resolveAlarmSentAt(row)
	row.ReportDelaySeconds = buildDelaySeconds(
		row.AlarmLatencyMillis,
		row.ReportActualPublishedAt,
		row.ReportAlarmSentAt,
	)
	applyTimeline(&row, timelineIndex[buildKey(row.ChannelID, row.AlarmType, row.ContentID)])
	return row
}

func applyTimeline(
	row *Row,
	timeline outbox.PostDeliveryTimeline,
) {
	if row == nil {
		return
	}
	row.PublishToDetectMillis = shared.CloneSendCountInt64(timeline.PublishToDetectMillis)
	row.DelaySource = timeline.DelaySource
	row.QueueWaitMillis = shared.CloneSendCountInt64(timeline.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(timeline.RetryAccumulationMillis)
	row.JobFailureDetected = timeline.JobFailureDetected
	row.InternalDelayCause = timeline.InternalDelayCause
	row.LatencyClassification = shared.CloneLatencyClassification(timeline.LatencyClassification)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
}

func accumulateSummary(
	summary *Summary,
	row Row,
) {
	if summary == nil {
		return
	}
	summary.PostCount++
	accumulateSendOutcome(summary, row)
	accumulateDelaySource(summary, row.DelaySource)
	accumulateInternalDelayCause(summary, row.InternalDelayCause)
}

func accumulateSendOutcome(
	summary *Summary,
	row Row,
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

func accumulateDelaySource(summary *Summary, delaySource outbox.PostDelaySource) {
	switch delaySource {
	case outbox.PostDelaySourceExternalCollection:
		summary.ExternalCollectionSourcePostCount++
	case outbox.PostDelaySourceInternalDelivery:
		summary.InternalDeliverySourcePostCount++
	case outbox.PostDelaySourceMixed:
		summary.MixedDelaySourcePostCount++
	}
}

func accumulateInternalDelayCause(summary *Summary, internalDelayCause outbox.PostInternalDelayCause) {
	switch internalDelayCause {
	case outbox.PostInternalDelayCauseQueueWait:
		summary.QueueWaitCausePostCount++
	case outbox.PostInternalDelayCauseRetryAccumulation:
		summary.RetryAccumulationCausePostCount++
	case outbox.PostInternalDelayCauseJobFailure:
		summary.JobFailureCausePostCount++
	}
}

func buildDeliveryTimelineIndex(timelineRows []outbox.PostDeliveryTimeline) map[sendCountKey]outbox.PostDeliveryTimeline {
	timelineIndex := make(map[sendCountKey]outbox.PostDeliveryTimeline, len(timelineRows))
	for i := range timelineRows {
		timeline := normalizeDeliveryTimeline(timelineRows[i])
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

func normalizePostSendCount(row outbox.PostSendCount) outbox.PostSendCount {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.ActualPublishedAt = shared.CloneSendCountTime(row.ActualPublishedAt)
	row.DetectedAt = shared.CloneSendCountTime(row.DetectedAt)
	row.AlarmSentAt = shared.CloneSendCountTime(row.AlarmSentAt)
	row.FirstEventAt = shared.CloneSendCountTime(row.FirstEventAt)
	row.LastEventAt = shared.CloneSendCountTime(row.LastEventAt)
	row.FirstSuccessAt = shared.CloneSendCountTime(row.FirstSuccessAt)
	row.LastSuccessAt = shared.CloneSendCountTime(row.LastSuccessAt)
	return row
}

func normalizeDeliveryTimeline(row outbox.PostDeliveryTimeline) outbox.PostDeliveryTimeline {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PublishToDetectMillis = shared.CloneSendCountInt64(row.PublishToDetectMillis)
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.QueueWaitMillis = shared.CloneSendCountInt64(row.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(row.RetryAccumulationMillis)
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	row.LatencyClassification = shared.CloneLatencyClassification(row.LatencyClassification)
	return row
}

func normalizeQuery(query Query) Query {
	query.Mode = QueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func rowSortTime(row Row) time.Time {
	for _, candidate := range []*time.Time{row.LastSuccessAt, row.LastEventAt, row.AlarmSentAt, row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

func resolveAlarmSentAt(row Row) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return shared.CloneSendCountTime(candidate)
		}
	}
	return nil
}

func resolvePostID(row Row) string {
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

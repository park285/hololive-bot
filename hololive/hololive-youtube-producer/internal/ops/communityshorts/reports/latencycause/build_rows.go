package latencycause

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	deliverytimeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/shared"
)

func isExceeded(row *analytics.PostSendCount) bool {
	return row != nil && row.AlarmLatencyExceeded != nil && *row.AlarmLatencyExceeded
}

func resolveObservedAt(row *analytics.PostSendCount) (time.Time, error) {
	if row == nil {
		return time.Time{}, fmt.Errorf("observed at is empty")
	}
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt.UTC(), nil
	}
	if row.DetectedAt != nil {
		return row.DetectedAt.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("observed at is empty")
}

func buildRow(
	sendCount *analytics.PostSendCount,
	observedAt time.Time,
	timeline *deliverytimeline.PostDeliveryTimeline,
	hasTimeline bool,
) Row {
	row := newRow(sendCount, observedAt)

	if !hasTimeline {
		return finalizeRow(&row)
	}

	return applyTimeline(&row, timeline)
}

func newRow(sendCount *analytics.PostSendCount, observedAt time.Time) Row {
	if sendCount == nil {
		return Row{
			ObservedAt:            shared.CloneSendCountTime(&observedAt),
			DelaySource:           deliverytimeline.PostDelaySourceNone,
			InternalDelayCause:    deliverytimeline.PostInternalDelayCauseNone,
			InternalCauseJudgment: InternalCauseJudgmentNonInternal,
			LatencyClassification: defaultLatencyClassification(),
		}
	}
	return Row{
		AlarmType:             sendCount.AlarmType,
		ChannelID:             strings.TrimSpace(sendCount.ChannelID),
		PostID:                resolvePostID(sendCount),
		ContentID:             strings.TrimSpace(sendCount.ContentID),
		ObservedAt:            shared.CloneSendCountTime(&observedAt),
		ActualPublishedAt:     shared.CloneSendCountTime(sendCount.ActualPublishedAt),
		DetectedAt:            shared.CloneSendCountTime(sendCount.DetectedAt),
		AlarmSentAt:           shared.CloneSendCountTime(sendCount.AlarmSentAt),
		AlarmLatencyMillis:    shared.CloneSendCountInt64(sendCount.AlarmLatencyMillis),
		DelaySource:           deliverytimeline.PostDelaySourceNone,
		InternalDelayCause:    deliverytimeline.PostInternalDelayCauseNone,
		InternalCauseJudgment: InternalCauseJudgmentNonInternal,
		LatencyClassification: defaultLatencyClassification(),
	}
}

func defaultLatencyClassification() deliverytimeline.PostLatencyClassificationResult {
	return deliverytimeline.PostLatencyClassificationResult{
		Status:             deliverytimeline.PostLatencyClassificationStatusInsufficientEvidence,
		ThresholdMillis:    int64((2 * time.Minute) / time.Millisecond),
		DelaySource:        deliverytimeline.PostDelaySourceNone,
		InternalDelayCause: deliverytimeline.PostInternalDelayCauseNone,
	}
}

func applyTimeline(row *Row, deliveryTimeline *deliverytimeline.PostDeliveryTimeline) Row {
	if row == nil {
		return Row{}
	}
	if deliveryTimeline == nil {
		return finalizeRow(row)
	}
	classification := normalizeClassification(&deliveryTimeline.LatencyClassification)
	row.PublishToDetectMillis = shared.CloneSendCountInt64(deliveryTimeline.PublishToDetectMillis)
	row.InternalLatencyMillis = shared.CloneSendCountInt64(deliveryTimeline.InternalLatencyMillis)
	row.QueueWaitMillis = shared.CloneSendCountInt64(deliveryTimeline.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(deliveryTimeline.RetryAccumulationMillis)
	row.JobFailureDetected = deliveryTimeline.JobFailureDetected
	row.DelaySource = deliveryTimeline.DelaySource
	if row.DelaySource == "" {
		row.DelaySource = classification.DelaySource
	}
	if row.DelaySource == "" {
		row.DelaySource = deliverytimeline.PostDelaySourceNone
	}
	row.InternalDelayCause = deliveryTimeline.InternalDelayCause
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = classification.InternalDelayCause
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = deliverytimeline.PostInternalDelayCauseNone
	}
	classification.DelaySource = row.DelaySource
	classification.InternalDelayCause = row.InternalDelayCause
	row.LatencyClassification = classification
	return finalizeRow(row)
}

func normalizeClassification(
	classification *deliverytimeline.PostLatencyClassificationResult,
) deliverytimeline.PostLatencyClassificationResult {
	normalized := shared.CloneLatencyClassification(classification)
	if normalized.Status == "" {
		normalized.Status = deliverytimeline.PostLatencyClassificationStatusInsufficientEvidence
	}
	if normalized.ThresholdMillis <= 0 {
		normalized.ThresholdMillis = int64((2 * time.Minute) / time.Millisecond)
	}
	if normalized.DelaySource == "" {
		normalized.DelaySource = deliverytimeline.PostDelaySourceNone
	}
	if normalized.InternalDelayCause == "" {
		normalized.InternalDelayCause = deliverytimeline.PostInternalDelayCauseNone
	}
	return normalized
}

func finalizeRow(row *Row) Row {
	if row == nil {
		return Row{}
	}
	row.InternalCauseJudgment, row.InternalCauseBasis = classifyInternalJudgment(row)
	row.CauseEvidence = buildEvidence(row)
	return *row
}

func sortedRows(rows []Row) []Row {
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return compareRows(&sorted[left], &sorted[right])
	})
	return sorted
}

func compareRows(left, right *Row) bool {
	leftTime := sortTime(left)
	rightTime := sortTime(right)
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	if left.AlarmType != right.AlarmType {
		return left.AlarmType < right.AlarmType
	}
	if left.ChannelID != right.ChannelID {
		return left.ChannelID < right.ChannelID
	}
	if left.PostID != right.PostID {
		return left.PostID < right.PostID
	}
	return left.ContentID < right.ContentID
}

func sortTime(row *Row) time.Time {
	if row == nil {
		return time.Time{}
	}
	for _, candidate := range []*time.Time{row.ObservedAt, row.AlarmSentAt, row.DetectedAt, row.ActualPublishedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

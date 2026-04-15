package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func BuildCommunityShortsLatencyCauseReport(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyCauseReport, error) {
	return BuildCommunityShortsLatencyCauseReportWithQuery(
		sendCountRows,
		timelineRows,
		CommunityShortsLatencyCauseQuery{Mode: communityShortsLatencyCauseQueryModeRecent},
		generatedAt,
		periods,
	)
}

func BuildCommunityShortsLatencyCauseReportWithQuery(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query CommunityShortsLatencyCauseQuery,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyCauseReport, error) {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedPeriods, requestedPeriods, err := normalizeCommunityShortsLatencyCausePeriods(periods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: %w", err)
	}
	query = withCommunityShortsLatencyCauseQueryWindow(normalizeCommunityShortsLatencyCauseQuery(query), normalizedPeriods)
	if query.Mode == "" {
		query.Mode = communityShortsLatencyCauseQueryModeRecent
	}

	summaries, err := outbox.BuildPostLatencyPeriodSummaries(sendCountRows, normalizedPeriods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: build post latency period summaries: %w", err)
	}

	periodRows := make([][]CommunityShortsLatencyCauseRow, len(normalizedPeriods))
	timelineIndex := buildCommunityShortsDeliveryTimelineIndex(timelineRows)

	for i := range sendCountRows {
		sendCount := normalizeCommunityShortsPostSendCount(sendCountRows[i])
		if !isCommunityShortsLatencyCauseExceeded(sendCount) {
			continue
		}

		observedAt, err := resolveCommunityShortsLatencyCauseObservedAt(sendCount)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("build community shorts latency cause report: post[%d] %s: %w", i, strings.TrimSpace(sendCount.ContentID), err)
		}

		key := buildCommunityShortsSendCountKey(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
		timeline, hasTimeline := timelineIndex[key]
		row := buildCommunityShortsLatencyCauseRow(sendCount, observedAt, timeline, hasTimeline)
		for periodIndex := range normalizedPeriods {
			if observedAt.Before(normalizedPeriods[periodIndex].StartAt) || !observedAt.Before(normalizedPeriods[periodIndex].EndAt) {
				continue
			}
			periodRows[periodIndex] = append(periodRows[periodIndex], row)
		}
	}

	periodViews := make([]CommunityShortsLatencyCausePeriodView, 0, len(summaries))
	for i := range summaries {
		rows := append([]CommunityShortsLatencyCauseRow(nil), periodRows[i]...)
		sort.SliceStable(rows, func(left, right int) bool {
			leftTime := communityShortsLatencyCauseSortTime(rows[left])
			rightTime := communityShortsLatencyCauseSortTime(rows[right])
			if !leftTime.Equal(rightTime) {
				return leftTime.After(rightTime)
			}
			if rows[left].AlarmType != rows[right].AlarmType {
				return rows[left].AlarmType < rows[right].AlarmType
			}
			if rows[left].ChannelID != rows[right].ChannelID {
				return rows[left].ChannelID < rows[right].ChannelID
			}
			if rows[left].PostID != rows[right].PostID {
				return rows[left].PostID < rows[right].PostID
			}
			return rows[left].ContentID < rows[right].ContentID
		})
		periodViews = append(periodViews, CommunityShortsLatencyCausePeriodView{
			Summary:      cloneCommunityShortsLatencyPeriodSummary(summaries[i]),
			CauseSummary: buildCommunityShortsLatencyCauseSummary(rows),
			Rows:         rows,
		})
	}

	return CommunityShortsLatencyCauseReport{
		GeneratedAt:      generatedAt,
		Query:            query,
		ObservedAtBasis:  communityShortsLatencyCauseObservedAtBasis,
		ThresholdMillis:  int64((2 * time.Minute) / time.Millisecond),
		Verification:     buildCommunityShortsLatencyCauseVerification(),
		RequestedPeriods: requestedPeriods,
		Periods:          periodViews,
	}, nil
}

func normalizeCommunityShortsLatencyCauseQuery(query CommunityShortsLatencyCauseQuery) CommunityShortsLatencyCauseQuery {
	query.Mode = CommunityShortsLatencyCauseQueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func withCommunityShortsLatencyCauseQueryWindow(
	query CommunityShortsLatencyCauseQuery,
	periods []outbox.PostLatencyPeriod,
) CommunityShortsLatencyCauseQuery {
	if query.WindowStart == nil {
		if startAt := earliestCommunityShortsLatencyCausePeriodStart(periods); !startAt.IsZero() {
			query.WindowStart = cloneCommunityShortsSendCountTime(&startAt)
		}
	}
	if query.WindowEnd == nil {
		if endAt := latestCommunityShortsLatencyCausePeriodEnd(periods); !endAt.IsZero() {
			query.WindowEnd = cloneCommunityShortsSendCountTime(&endAt)
		}
	}
	return query
}

func normalizeCommunityShortsLatencyCausePeriods(
	periods []outbox.PostLatencyPeriod,
) ([]outbox.PostLatencyPeriod, []CommunityShortsLatencyPeriodSpec, error) {
	if len(periods) == 0 {
		return []outbox.PostLatencyPeriod{}, []CommunityShortsLatencyPeriodSpec{}, nil
	}

	normalized := make([]outbox.PostLatencyPeriod, 0, len(periods))
	requestedPeriods := make([]CommunityShortsLatencyPeriodSpec, 0, len(periods))
	seenLabels := make(map[string]struct{}, len(periods))
	for i := range periods {
		label := strings.TrimSpace(periods[i].Label)
		if label == "" {
			return nil, nil, fmt.Errorf("period at index %d: label is empty", i)
		}
		if periods[i].StartAt.IsZero() {
			return nil, nil, fmt.Errorf("period %q: start at is empty", label)
		}
		if periods[i].EndAt.IsZero() {
			return nil, nil, fmt.Errorf("period %q: end at is empty", label)
		}
		startAt := periods[i].StartAt.UTC()
		endAt := periods[i].EndAt.UTC()
		if !endAt.After(startAt) {
			return nil, nil, fmt.Errorf("period %q: end at must be after start at", label)
		}
		if _, exists := seenLabels[label]; exists {
			return nil, nil, fmt.Errorf("period %q: duplicate label", label)
		}
		seenLabels[label] = struct{}{}
		normalized = append(normalized, outbox.PostLatencyPeriod{Label: label, StartAt: startAt, EndAt: endAt})
		requestedPeriods = append(requestedPeriods, CommunityShortsLatencyPeriodSpec{Label: label, Window: endAt.Sub(startAt)})
	}
	return normalized, requestedPeriods, nil
}

func earliestCommunityShortsLatencyCausePeriodStart(periods []outbox.PostLatencyPeriod) time.Time {
	if len(periods) == 0 {
		return time.Time{}
	}
	startAt := periods[0].StartAt
	for i := 1; i < len(periods); i++ {
		if periods[i].StartAt.Before(startAt) {
			startAt = periods[i].StartAt
		}
	}
	return startAt.UTC()
}

func latestCommunityShortsLatencyCausePeriodEnd(periods []outbox.PostLatencyPeriod) time.Time {
	if len(periods) == 0 {
		return time.Time{}
	}
	endAt := periods[0].EndAt
	for i := 1; i < len(periods); i++ {
		if periods[i].EndAt.After(endAt) {
			endAt = periods[i].EndAt
		}
	}
	return endAt.UTC()
}

func isCommunityShortsLatencyCauseExceeded(row outbox.PostSendCount) bool {
	return row.AlarmLatencyExceeded != nil && *row.AlarmLatencyExceeded
}

func resolveCommunityShortsLatencyCauseObservedAt(row outbox.PostSendCount) (time.Time, error) {
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt.UTC(), nil
	}
	if row.DetectedAt != nil {
		return row.DetectedAt.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("observed at is empty")
}

func buildCommunityShortsLatencyCauseRow(
	sendCount outbox.PostSendCount,
	observedAt time.Time,
	timeline outbox.PostDeliveryTimeline,
	hasTimeline bool,
) CommunityShortsLatencyCauseRow {
	classification := outbox.PostLatencyClassificationResult{
		Status:             outbox.PostLatencyClassificationStatusInsufficientEvidence,
		ThresholdMillis:    int64((2 * time.Minute) / time.Millisecond),
		DelaySource:        outbox.PostDelaySourceNone,
		InternalDelayCause: outbox.PostInternalDelayCauseNone,
	}

	row := CommunityShortsLatencyCauseRow{
		AlarmType:             sendCount.AlarmType,
		ChannelID:             strings.TrimSpace(sendCount.ChannelID),
		PostID:                resolveCommunityShortsSendCountPostID(CommunityShortsSendCountRow{PostSendCount: sendCount}),
		ContentID:             strings.TrimSpace(sendCount.ContentID),
		ObservedAt:            cloneCommunityShortsSendCountTime(&observedAt),
		ActualPublishedAt:     cloneCommunityShortsSendCountTime(sendCount.ActualPublishedAt),
		DetectedAt:            cloneCommunityShortsSendCountTime(sendCount.DetectedAt),
		AlarmSentAt:           cloneCommunityShortsSendCountTime(sendCount.AlarmSentAt),
		AlarmLatencyMillis:    cloneCommunityShortsSendCountInt64(sendCount.AlarmLatencyMillis),
		DelaySource:           outbox.PostDelaySourceNone,
		InternalDelayCause:    outbox.PostInternalDelayCauseNone,
		InternalCauseJudgment: CommunityShortsInternalCauseJudgmentNonInternal,
		LatencyClassification: classification,
	}

	if !hasTimeline {
		row.InternalCauseJudgment, row.InternalCauseBasis = classifyCommunityShortsLatencyCauseInternalJudgment(row)
		row.CauseEvidence = buildCommunityShortsLatencyCauseEvidence(row)
		return row
	}

	classification = cloneCommunityShortsLatencyClassification(timeline.LatencyClassification)
	if classification.Status == "" {
		classification.Status = outbox.PostLatencyClassificationStatusInsufficientEvidence
	}
	if classification.ThresholdMillis <= 0 {
		classification.ThresholdMillis = int64((2 * time.Minute) / time.Millisecond)
	}
	if classification.DelaySource == "" {
		classification.DelaySource = outbox.PostDelaySourceNone
	}
	if classification.InternalDelayCause == "" {
		classification.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}

	row.PublishToDetectMillis = cloneCommunityShortsSendCountInt64(timeline.PublishToDetectMillis)
	row.InternalLatencyMillis = cloneCommunityShortsSendCountInt64(timeline.InternalLatencyMillis)
	row.QueueWaitMillis = cloneCommunityShortsSendCountInt64(timeline.QueueWaitMillis)
	row.RetryAccumulationMillis = cloneCommunityShortsSendCountInt64(timeline.RetryAccumulationMillis)
	row.JobFailureDetected = timeline.JobFailureDetected
	row.DelaySource = timeline.DelaySource
	if row.DelaySource == "" {
		row.DelaySource = classification.DelaySource
	}
	if row.DelaySource == "" {
		row.DelaySource = outbox.PostDelaySourceNone
	}
	row.InternalDelayCause = timeline.InternalDelayCause
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = classification.InternalDelayCause
	}
	if row.InternalDelayCause == "" {
		row.InternalDelayCause = outbox.PostInternalDelayCauseNone
	}
	classification.DelaySource = row.DelaySource
	classification.InternalDelayCause = row.InternalDelayCause
	row.LatencyClassification = classification
	row.InternalCauseJudgment, row.InternalCauseBasis = classifyCommunityShortsLatencyCauseInternalJudgment(row)
	row.CauseEvidence = buildCommunityShortsLatencyCauseEvidence(row)
	return row
}

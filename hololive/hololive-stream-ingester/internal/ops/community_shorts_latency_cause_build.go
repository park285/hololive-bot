package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

type communityShortsLatencyCauseBuildContext struct {
	generatedAt       time.Time
	query             CommunityShortsLatencyCauseQuery
	normalizedPeriods []outbox.PostLatencyPeriod
	requestedPeriods  []CommunityShortsLatencyPeriodSpec
	summaries         []outbox.PostLatencyPeriodSummary
	periodRows        [][]CommunityShortsLatencyCauseRow
}

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
	buildCtx, err := newCommunityShortsLatencyCauseBuildContext(sendCountRows, query, generatedAt, periods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, err
	}

	timelineIndex := buildCommunityShortsDeliveryTimelineIndex(timelineRows)
	if err := buildCtx.addLatencyCauseRows(sendCountRows, timelineIndex); err != nil {
		return CommunityShortsLatencyCauseReport{}, err
	}

	return buildCtx.report(), nil
}

func newCommunityShortsLatencyCauseBuildContext(
	sendCountRows []outbox.PostSendCount,
	query CommunityShortsLatencyCauseQuery,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (communityShortsLatencyCauseBuildContext, error) {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedPeriods, requestedPeriods, err := normalizeCommunityShortsLatencyCausePeriods(periods)
	if err != nil {
		return communityShortsLatencyCauseBuildContext{}, fmt.Errorf("build community shorts latency cause report: %w", err)
	}
	query = withCommunityShortsLatencyCauseQueryWindow(normalizeCommunityShortsLatencyCauseQuery(query), normalizedPeriods)
	if query.Mode == "" {
		query.Mode = communityShortsLatencyCauseQueryModeRecent
	}

	summaries, err := outbox.BuildPostLatencyPeriodSummaries(sendCountRows, normalizedPeriods)
	if err != nil {
		return communityShortsLatencyCauseBuildContext{}, fmt.Errorf("build community shorts latency cause report: build post latency period summaries: %w", err)
	}

	return communityShortsLatencyCauseBuildContext{
		generatedAt:       generatedAt,
		query:             query,
		normalizedPeriods: normalizedPeriods,
		requestedPeriods:  requestedPeriods,
		summaries:         summaries,
		periodRows:        make([][]CommunityShortsLatencyCauseRow, len(normalizedPeriods)),
	}, nil
}

func (buildCtx *communityShortsLatencyCauseBuildContext) addLatencyCauseRows(
	sendCountRows []outbox.PostSendCount,
	timelineIndex map[communityShortsSendCountKey]outbox.PostDeliveryTimeline,
) error {
	for i := range sendCountRows {
		if err := buildCtx.addLatencyCauseRow(i, sendCountRows[i], timelineIndex); err != nil {
			return err
		}
	}
	return nil
}

func (buildCtx *communityShortsLatencyCauseBuildContext) addLatencyCauseRow(
	index int,
	rawSendCount outbox.PostSendCount,
	timelineIndex map[communityShortsSendCountKey]outbox.PostDeliveryTimeline,
) error {
	sendCount := normalizeCommunityShortsPostSendCount(rawSendCount)
	if !isCommunityShortsLatencyCauseExceeded(sendCount) {
		return nil
	}

	observedAt, err := resolveCommunityShortsLatencyCauseObservedAt(sendCount)
	if err != nil {
		return fmt.Errorf("build community shorts latency cause report: post[%d] %s: %w", index, strings.TrimSpace(sendCount.ContentID), err)
	}

	key := buildCommunityShortsSendCountKey(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
	timeline, hasTimeline := timelineIndex[key]
	row := buildCommunityShortsLatencyCauseRow(sendCount, observedAt, timeline, hasTimeline)
	buildCtx.addLatencyCauseRowToPeriods(observedAt, row)
	return nil
}

func (buildCtx *communityShortsLatencyCauseBuildContext) addLatencyCauseRowToPeriods(
	observedAt time.Time,
	row CommunityShortsLatencyCauseRow,
) {
	for periodIndex := range buildCtx.normalizedPeriods {
		if isWithinLatencyCausePeriod(observedAt, buildCtx.normalizedPeriods[periodIndex]) {
			buildCtx.periodRows[periodIndex] = append(buildCtx.periodRows[periodIndex], row)
		}
	}
}

func isWithinLatencyCausePeriod(observedAt time.Time, period outbox.PostLatencyPeriod) bool {
	return !observedAt.Before(period.StartAt) && observedAt.Before(period.EndAt)
}

func (buildCtx communityShortsLatencyCauseBuildContext) report() CommunityShortsLatencyCauseReport {
	return CommunityShortsLatencyCauseReport{
		GeneratedAt:      buildCtx.generatedAt,
		Query:            buildCtx.query,
		ObservedAtBasis:  communityShortsLatencyCauseObservedAtBasis,
		ThresholdMillis:  int64((2 * time.Minute) / time.Millisecond),
		Verification:     buildCommunityShortsLatencyCauseVerification(),
		RequestedPeriods: buildCtx.requestedPeriods,
		Periods:          buildCtx.periodViews(),
	}
}

func (buildCtx communityShortsLatencyCauseBuildContext) periodViews() []CommunityShortsLatencyCausePeriodView {
	periodViews := make([]CommunityShortsLatencyCausePeriodView, 0, len(buildCtx.summaries))
	for i := range buildCtx.summaries {
		rows := sortedCommunityShortsLatencyCauseRows(buildCtx.periodRows[i])
		periodViews = append(periodViews, CommunityShortsLatencyCausePeriodView{
			Summary:      cloneCommunityShortsLatencyPeriodSummary(buildCtx.summaries[i]),
			CauseSummary: buildCommunityShortsLatencyCauseSummary(rows),
			Rows:         rows,
		})
	}
	return periodViews
}

func sortedCommunityShortsLatencyCauseRows(rows []CommunityShortsLatencyCauseRow) []CommunityShortsLatencyCauseRow {
	sortedRows := append([]CommunityShortsLatencyCauseRow(nil), rows...)
	sort.SliceStable(sortedRows, func(left, right int) bool {
		return compareCommunityShortsLatencyCauseRows(sortedRows[left], sortedRows[right])
	})
	return sortedRows
}

func compareCommunityShortsLatencyCauseRows(left, right CommunityShortsLatencyCauseRow) bool {
	leftTime := communityShortsLatencyCauseSortTime(left)
	rightTime := communityShortsLatencyCauseSortTime(right)
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
		period, requestedPeriod, err := normalizeCommunityShortsLatencyCausePeriod(i, periods[i], seenLabels)
		if err != nil {
			return nil, nil, err
		}
		normalized = append(normalized, period)
		requestedPeriods = append(requestedPeriods, requestedPeriod)
	}
	return normalized, requestedPeriods, nil
}

func normalizeCommunityShortsLatencyCausePeriod(
	index int,
	period outbox.PostLatencyPeriod,
	seenLabels map[string]struct{},
) (outbox.PostLatencyPeriod, CommunityShortsLatencyPeriodSpec, error) {
	label := strings.TrimSpace(period.Label)
	if label == "" {
		return outbox.PostLatencyPeriod{}, CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("period at index %d: label is empty", index)
	}
	if err := validateCommunityShortsLatencyCausePeriodBounds(label, period); err != nil {
		return outbox.PostLatencyPeriod{}, CommunityShortsLatencyPeriodSpec{}, err
	}
	if _, exists := seenLabels[label]; exists {
		return outbox.PostLatencyPeriod{}, CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("period %q: duplicate label", label)
	}
	seenLabels[label] = struct{}{}

	startAt := period.StartAt.UTC()
	endAt := period.EndAt.UTC()
	return outbox.PostLatencyPeriod{Label: label, StartAt: startAt, EndAt: endAt},
		CommunityShortsLatencyPeriodSpec{Label: label, Window: endAt.Sub(startAt)},
		nil
}

func validateCommunityShortsLatencyCausePeriodBounds(label string, period outbox.PostLatencyPeriod) error {
	if period.StartAt.IsZero() {
		return fmt.Errorf("period %q: start at is empty", label)
	}
	if period.EndAt.IsZero() {
		return fmt.Errorf("period %q: end at is empty", label)
	}
	if !period.EndAt.UTC().After(period.StartAt.UTC()) {
		return fmt.Errorf("period %q: end at must be after start at", label)
	}
	return nil
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
	row := newCommunityShortsLatencyCauseRow(sendCount, observedAt)

	if !hasTimeline {
		return finalizeCommunityShortsLatencyCauseRow(row)
	}

	return applyCommunityShortsLatencyCauseTimeline(row, timeline)
}

func newCommunityShortsLatencyCauseRow(sendCount outbox.PostSendCount, observedAt time.Time) CommunityShortsLatencyCauseRow {
	return CommunityShortsLatencyCauseRow{
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
		LatencyClassification: defaultCommunityShortsLatencyClassification(),
	}
}

func defaultCommunityShortsLatencyClassification() outbox.PostLatencyClassificationResult {
	return outbox.PostLatencyClassificationResult{
		Status:             outbox.PostLatencyClassificationStatusInsufficientEvidence,
		ThresholdMillis:    int64((2 * time.Minute) / time.Millisecond),
		DelaySource:        outbox.PostDelaySourceNone,
		InternalDelayCause: outbox.PostInternalDelayCauseNone,
	}
}

func applyCommunityShortsLatencyCauseTimeline(row CommunityShortsLatencyCauseRow, timeline outbox.PostDeliveryTimeline) CommunityShortsLatencyCauseRow {
	classification := normalizeCommunityShortsLatencyClassification(timeline.LatencyClassification)
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
	return finalizeCommunityShortsLatencyCauseRow(row)
}

func normalizeCommunityShortsLatencyClassification(
	classification outbox.PostLatencyClassificationResult,
) outbox.PostLatencyClassificationResult {
	classification = cloneCommunityShortsLatencyClassification(classification)
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
	return classification
}

func finalizeCommunityShortsLatencyCauseRow(row CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseRow {
	row.InternalCauseJudgment, row.InternalCauseBasis = classifyCommunityShortsLatencyCauseInternalJudgment(row)
	row.CauseEvidence = buildCommunityShortsLatencyCauseEvidence(row)
	return row
}

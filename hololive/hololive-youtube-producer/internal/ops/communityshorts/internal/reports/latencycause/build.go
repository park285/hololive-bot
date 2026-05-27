package latencycause

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type buildContext struct {
	generatedAt       time.Time
	query             Query
	normalizedPeriods []outbox.PostLatencyPeriod
	requestedPeriods  []PeriodSpec
	summaries         []outbox.PostLatencyPeriodSummary
	periodRows        [][]Row
}

func Build(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (Report, error) {
	return BuildWithQuery(
		sendCountRows,
		timelineRows,
		Query{Mode: queryModeRecent},
		generatedAt,
		periods,
	)
}

func BuildWithQuery(
	sendCountRows []outbox.PostSendCount,
	timelineRows []outbox.PostDeliveryTimeline,
	query Query,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (Report, error) {
	buildCtx, err := newBuildContext(sendCountRows, query, generatedAt, periods)
	if err != nil {
		return Report{}, err
	}

	timelineIndex := buildTimelineIndex(timelineRows)
	if err := buildCtx.addRows(sendCountRows, timelineIndex); err != nil {
		return Report{}, err
	}

	return buildCtx.report(), nil
}

func newBuildContext(
	sendCountRows []outbox.PostSendCount,
	query Query,
	generatedAt time.Time,
	periods []outbox.PostLatencyPeriod,
) (buildContext, error) {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedPeriods, requestedPeriods, err := normalizePeriods(periods)
	if err != nil {
		return buildContext{}, fmt.Errorf("build community shorts latency cause report: %w", err)
	}
	query = withQueryWindow(normalizeQuery(query), normalizedPeriods)
	if query.Mode == "" {
		query.Mode = queryModeRecent
	}

	summaries, err := outbox.BuildPostLatencyPeriodSummaries(sendCountRows, normalizedPeriods)
	if err != nil {
		return buildContext{}, fmt.Errorf("build community shorts latency cause report: build post latency period summaries: %w", err)
	}

	return buildContext{
		generatedAt:       generatedAt,
		query:             query,
		normalizedPeriods: normalizedPeriods,
		requestedPeriods:  requestedPeriods,
		summaries:         summaries,
		periodRows:        make([][]Row, len(normalizedPeriods)),
	}, nil
}

func (ctx *buildContext) addRows(
	sendCountRows []outbox.PostSendCount,
	timelineIndex map[timelineKey]outbox.PostDeliveryTimeline,
) error {
	for i := range sendCountRows {
		if err := ctx.addRow(i, sendCountRows[i], timelineIndex); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *buildContext) addRow(
	index int,
	rawSendCount outbox.PostSendCount,
	timelineIndex map[timelineKey]outbox.PostDeliveryTimeline,
) error {
	sendCount := normalizePostSendCount(rawSendCount)
	if !isExceeded(sendCount) {
		return nil
	}

	observedAt, err := resolveObservedAt(sendCount)
	if err != nil {
		return fmt.Errorf("build community shorts latency cause report: post[%d] %s: %w", index, strings.TrimSpace(sendCount.ContentID), err)
	}

	key := buildTimelineKey(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
	timeline, hasTimeline := timelineIndex[key]
	row := buildRow(sendCount, observedAt, timeline, hasTimeline)
	ctx.addRowToPeriods(observedAt, row)
	return nil
}

func (ctx *buildContext) addRowToPeriods(observedAt time.Time, row Row) {
	for periodIndex := range ctx.normalizedPeriods {
		if isWithinPeriod(observedAt, ctx.normalizedPeriods[periodIndex]) {
			ctx.periodRows[periodIndex] = append(ctx.periodRows[periodIndex], row)
		}
	}
}

func isWithinPeriod(observedAt time.Time, period outbox.PostLatencyPeriod) bool {
	return !observedAt.Before(period.StartAt) && observedAt.Before(period.EndAt)
}

func (ctx buildContext) report() Report {
	return Report{
		GeneratedAt:      ctx.generatedAt,
		Query:            ctx.query,
		ObservedAtBasis:  observedAtBasis,
		ThresholdMillis:  int64((2 * time.Minute) / time.Millisecond),
		Verification:     buildVerification(),
		RequestedPeriods: ctx.requestedPeriods,
		Periods:          ctx.periodViews(),
	}
}

func (ctx buildContext) periodViews() []PeriodView {
	views := make([]PeriodView, 0, len(ctx.summaries))
	for i := range ctx.summaries {
		rows := sortedRows(ctx.periodRows[i])
		views = append(views, PeriodView{
			Summary:      clonePeriodSummary(ctx.summaries[i]),
			CauseSummary: buildCauseSummary(rows),
			Rows:         rows,
		})
	}
	return views
}

func normalizeQuery(query Query) Query {
	query.Mode = QueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func withQueryWindow(query Query, periods []outbox.PostLatencyPeriod) Query {
	if query.WindowStart == nil {
		if startAt := earliestPeriodStart(periods); !startAt.IsZero() {
			query.WindowStart = shared.CloneSendCountTime(&startAt)
		}
	}
	if query.WindowEnd == nil {
		if endAt := latestPeriodEnd(periods); !endAt.IsZero() {
			query.WindowEnd = shared.CloneSendCountTime(&endAt)
		}
	}
	return query
}

func latestPeriodEnd(periods []outbox.PostLatencyPeriod) time.Time {
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

func normalizePeriods(
	periods []outbox.PostLatencyPeriod,
) ([]outbox.PostLatencyPeriod, []PeriodSpec, error) {
	if len(periods) == 0 {
		return []outbox.PostLatencyPeriod{}, []PeriodSpec{}, nil
	}

	normalized := make([]outbox.PostLatencyPeriod, 0, len(periods))
	requestedPeriods := make([]PeriodSpec, 0, len(periods))
	seenLabels := make(map[string]struct{}, len(periods))
	for i := range periods {
		period, requestedPeriod, err := normalizePeriod(i, periods[i], seenLabels)
		if err != nil {
			return nil, nil, err
		}
		normalized = append(normalized, period)
		requestedPeriods = append(requestedPeriods, requestedPeriod)
	}
	return normalized, requestedPeriods, nil
}

func normalizePeriod(
	index int,
	period outbox.PostLatencyPeriod,
	seenLabels map[string]struct{},
) (outbox.PostLatencyPeriod, PeriodSpec, error) {
	label := strings.TrimSpace(period.Label)
	if label == "" {
		return outbox.PostLatencyPeriod{}, PeriodSpec{}, fmt.Errorf("period at index %d: label is empty", index)
	}
	if err := validatePeriodBounds(label, period); err != nil {
		return outbox.PostLatencyPeriod{}, PeriodSpec{}, err
	}
	if _, exists := seenLabels[label]; exists {
		return outbox.PostLatencyPeriod{}, PeriodSpec{}, fmt.Errorf("period %q: duplicate label", label)
	}
	seenLabels[label] = struct{}{}

	startAt := period.StartAt.UTC()
	endAt := period.EndAt.UTC()
	return outbox.PostLatencyPeriod{Label: label, StartAt: startAt, EndAt: endAt},
		PeriodSpec{Label: label, Window: endAt.Sub(startAt)},
		nil
}

func validatePeriodBounds(label string, period outbox.PostLatencyPeriod) error {
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

func isExceeded(row outbox.PostSendCount) bool {
	return row.AlarmLatencyExceeded != nil && *row.AlarmLatencyExceeded
}

func resolveObservedAt(row outbox.PostSendCount) (time.Time, error) {
	if row.ActualPublishedAt != nil {
		return row.ActualPublishedAt.UTC(), nil
	}
	if row.DetectedAt != nil {
		return row.DetectedAt.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("observed at is empty")
}

func buildRow(
	sendCount outbox.PostSendCount,
	observedAt time.Time,
	timeline outbox.PostDeliveryTimeline,
	hasTimeline bool,
) Row {
	row := newRow(sendCount, observedAt)

	if !hasTimeline {
		return finalizeRow(row)
	}

	return applyTimeline(row, timeline)
}

func newRow(sendCount outbox.PostSendCount, observedAt time.Time) Row {
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
		DelaySource:           outbox.PostDelaySourceNone,
		InternalDelayCause:    outbox.PostInternalDelayCauseNone,
		InternalCauseJudgment: InternalCauseJudgmentNonInternal,
		LatencyClassification: defaultLatencyClassification(),
	}
}

func defaultLatencyClassification() outbox.PostLatencyClassificationResult {
	return outbox.PostLatencyClassificationResult{
		Status:             outbox.PostLatencyClassificationStatusInsufficientEvidence,
		ThresholdMillis:    int64((2 * time.Minute) / time.Millisecond),
		DelaySource:        outbox.PostDelaySourceNone,
		InternalDelayCause: outbox.PostInternalDelayCauseNone,
	}
}

func applyTimeline(row Row, timeline outbox.PostDeliveryTimeline) Row {
	classification := normalizeClassification(timeline.LatencyClassification)
	row.PublishToDetectMillis = shared.CloneSendCountInt64(timeline.PublishToDetectMillis)
	row.InternalLatencyMillis = shared.CloneSendCountInt64(timeline.InternalLatencyMillis)
	row.QueueWaitMillis = shared.CloneSendCountInt64(timeline.QueueWaitMillis)
	row.RetryAccumulationMillis = shared.CloneSendCountInt64(timeline.RetryAccumulationMillis)
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
	return finalizeRow(row)
}

func normalizeClassification(
	classification outbox.PostLatencyClassificationResult,
) outbox.PostLatencyClassificationResult {
	classification = shared.CloneLatencyClassification(classification)
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

func finalizeRow(row Row) Row {
	row.InternalCauseJudgment, row.InternalCauseBasis = classifyInternalJudgment(row)
	row.CauseEvidence = buildEvidence(row)
	return row
}

func sortedRows(rows []Row) []Row {
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return compareRows(sorted[left], sorted[right])
	})
	return sorted
}

func compareRows(left, right Row) bool {
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

func sortTime(row Row) time.Time {
	for _, candidate := range []*time.Time{row.ObservedAt, row.AlarmSentAt, row.DetectedAt, row.ActualPublishedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return time.Time{}
}

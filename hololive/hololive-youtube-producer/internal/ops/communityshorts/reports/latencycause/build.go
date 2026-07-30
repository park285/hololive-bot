package latencycause

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/analytics"
	deliverytimeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/shared"
)

type buildContext struct {
	generatedAt       time.Time
	query             Query
	normalizedPeriods []analytics.PostLatencyPeriod
	requestedPeriods  []PeriodSpec
	summaries         []analytics.PostLatencyPeriodSummary
	periodRows        [][]Row
}

func Build(
	sendCountRows []analytics.PostSendCount,
	timelineRows []deliverytimeline.PostDeliveryTimeline,
	generatedAt time.Time,
	periods []analytics.PostLatencyPeriod,
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
	sendCountRows []analytics.PostSendCount,
	timelineRows []deliverytimeline.PostDeliveryTimeline,
	query Query,
	generatedAt time.Time,
	periods []analytics.PostLatencyPeriod,
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
	sendCountRows []analytics.PostSendCount,
	query Query,
	generatedAt time.Time,
	periods []analytics.PostLatencyPeriod,
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

	summaries, err := analytics.BuildPostLatencyPeriodSummaries(sendCountRows, normalizedPeriods)
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
	sendCountRows []analytics.PostSendCount,
	timelineIndex map[timelineKey]deliverytimeline.PostDeliveryTimeline,
) error {
	for i := range sendCountRows {
		if err := ctx.addRow(i, &sendCountRows[i], timelineIndex); err != nil {
			return err
		}
	}
	return nil
}

func (ctx *buildContext) addRow(
	index int,
	rawSendCount *analytics.PostSendCount,
	timelineIndex map[timelineKey]deliverytimeline.PostDeliveryTimeline,
) error {
	sendCount := normalizePostSendCount(rawSendCount)
	if !isExceeded(&sendCount) {
		return nil
	}

	observedAt, err := resolveObservedAt(&sendCount)
	if err != nil {
		return fmt.Errorf("build community shorts latency cause report: post[%d] %s: %w", index, strings.TrimSpace(sendCount.ContentID), err)
	}

	key := buildTimelineKey(sendCount.ChannelID, sendCount.AlarmType, sendCount.ContentID)
	timeline, hasTimeline := timelineIndex[key]
	row := buildRow(&sendCount, observedAt, &timeline, hasTimeline)
	ctx.addRowToPeriods(observedAt, &row)
	return nil
}

func (ctx *buildContext) addRowToPeriods(observedAt time.Time, row *Row) {
	if row == nil {
		return
	}
	for periodIndex := range ctx.normalizedPeriods {
		if isWithinPeriod(observedAt, ctx.normalizedPeriods[periodIndex]) {
			ctx.periodRows[periodIndex] = append(ctx.periodRows[periodIndex], *row)
		}
	}
}

func isWithinPeriod(observedAt time.Time, period analytics.PostLatencyPeriod) bool {
	return !observedAt.Before(period.StartAt) && observedAt.Before(period.EndAt)
}

func (ctx *buildContext) report() Report {
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

func (ctx *buildContext) periodViews() []PeriodView {
	views := make([]PeriodView, 0, len(ctx.summaries))
	for i := range ctx.summaries {
		rows := sortedRows(ctx.periodRows[i])
		views = append(views, PeriodView{
			Summary:      clonePeriodSummary(&ctx.summaries[i]),
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
	return query
}

func withQueryWindow(query Query, periods []analytics.PostLatencyPeriod) Query {
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

func latestPeriodEnd(periods []analytics.PostLatencyPeriod) time.Time {
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
	periods []analytics.PostLatencyPeriod,
) ([]analytics.PostLatencyPeriod, []PeriodSpec, error) {
	if len(periods) == 0 {
		return []analytics.PostLatencyPeriod{}, []PeriodSpec{}, nil
	}

	normalized := make([]analytics.PostLatencyPeriod, 0, len(periods))
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
	period analytics.PostLatencyPeriod,
	seenLabels map[string]struct{},
) (analytics.PostLatencyPeriod, PeriodSpec, error) {
	label := strings.TrimSpace(period.Label)
	if label == "" {
		return analytics.PostLatencyPeriod{}, PeriodSpec{}, fmt.Errorf("period at index %d: label is empty", index)
	}
	if err := validatePeriodBounds(label, period); err != nil {
		return analytics.PostLatencyPeriod{}, PeriodSpec{}, err
	}
	if _, exists := seenLabels[label]; exists {
		return analytics.PostLatencyPeriod{}, PeriodSpec{}, fmt.Errorf("period %q: duplicate label", label)
	}
	seenLabels[label] = struct{}{}

	startAt := period.StartAt.UTC()
	endAt := period.EndAt.UTC()
	return analytics.PostLatencyPeriod{Label: label, StartAt: startAt, EndAt: endAt},
		PeriodSpec{Label: label, Window: endAt.Sub(startAt)},
		nil
}

func validatePeriodBounds(label string, period analytics.PostLatencyPeriod) error {
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

package delivery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type PostLatencyPeriod struct {
	Label   string
	StartAt time.Time
	EndAt   time.Time
}

type PostLatencyPeriodSummary struct {
	Label                      string
	StartAt                    time.Time
	EndAt                      time.Time
	TotalPostCount             int64
	AlarmSentPostCount         int64
	PendingPostCount           int64
	LatencyMeasuredPostCount   int64
	WithinTargetPostCount      int64
	ExceededPostCount          int64
	CommunityPostCount         int64
	CommunityExceededPostCount int64
	ShortsPostCount            int64
	ShortsExceededPostCount    int64
	AverageLatencyMillis       *int64
	P95LatencyMillis           *int64
	MaxLatencyMillis           *int64
}

func (r *DeliveryTelemetryRepository) ListPostLatencyPeriodSummaries(ctx context.Context, periods []PostLatencyPeriod) ([]PostLatencyPeriodSummary, error) {
	normalizedPeriods, err := normalizePostLatencyPeriods(periods)
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: %w", err)
	}
	if len(normalizedPeriods) == 0 {
		return []PostLatencyPeriodSummary{}, nil
	}

	posts, err := r.ListPostSendCountsSince(ctx, earliestPostLatencyPeriodStart(normalizedPeriods))
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: load post send counts: %w", err)
	}

	summaries, err := BuildPostLatencyPeriodSummaries(posts, normalizedPeriods)
	if err != nil {
		return nil, fmt.Errorf("list post latency period summaries: %w", err)
	}

	return summaries, nil
}

func BuildPostLatencyPeriodSummaries(posts []PostSendCount, periods []PostLatencyPeriod) ([]PostLatencyPeriodSummary, error) {
	normalizedPeriods, err := normalizePostLatencyPeriods(periods)
	if err != nil {
		return nil, fmt.Errorf("build post latency period summaries: %w", err)
	}
	if len(normalizedPeriods) == 0 {
		return []PostLatencyPeriodSummary{}, nil
	}

	accumulators := newPostLatencyPeriodSummaryAccumulators(normalizedPeriods)

	for i := range posts {
		if err := addPostToLatencyPeriodSummaries(accumulators, normalizedPeriods, posts[i], i); err != nil {
			return nil, err
		}
	}

	return finalizePostLatencyPeriodSummaries(accumulators), nil
}

type postLatencyPeriodSummaryAccumulator struct {
	summary              PostLatencyPeriodSummary
	latencySumMillis     int64
	latencyMeasuredCount int64
	latencySamplesMillis []int64
	maxLatencyMillis     int64
}

func newPostLatencyPeriodSummaryAccumulators(periods []PostLatencyPeriod) []postLatencyPeriodSummaryAccumulator {
	accumulators := make([]postLatencyPeriodSummaryAccumulator, len(periods))
	for i := range periods {
		accumulators[i].summary = PostLatencyPeriodSummary{
			Label:   periods[i].Label,
			StartAt: periods[i].StartAt,
			EndAt:   periods[i].EndAt,
		}
	}
	return accumulators
}

func addPostToLatencyPeriodSummaries(
	accumulators []postLatencyPeriodSummaryAccumulator,
	periods []PostLatencyPeriod,
	post PostSendCount,
	index int,
) error {
	observedAt, err := postLatencyObservedAt(post)
	if err != nil {
		return fmt.Errorf("post[%d] %s: %w", index, strings.TrimSpace(post.ContentID), err)
	}

	for j := range periods {
		if !postObservedInLatencyPeriod(observedAt, periods[j]) {
			continue
		}
		if err := accumulators[j].add(post); err != nil {
			return fmt.Errorf("post[%d] %s: %w", index, strings.TrimSpace(post.ContentID), err)
		}
	}
	return nil
}

func postObservedInLatencyPeriod(observedAt time.Time, period PostLatencyPeriod) bool {
	return !observedAt.Before(period.StartAt) && observedAt.Before(period.EndAt)
}

func finalizePostLatencyPeriodSummaries(accumulators []postLatencyPeriodSummaryAccumulator) []PostLatencyPeriodSummary {
	summaries := make([]PostLatencyPeriodSummary, 0, len(accumulators))
	for i := range accumulators {
		summaries = append(summaries, accumulators[i].finalize())
	}
	return summaries
}

func (a *postLatencyPeriodSummaryAccumulator) add(post PostSendCount) error {
	a.summary.TotalPostCount++

	if err := a.addAlarmType(post.AlarmType); err != nil {
		return err
	}
	a.addAlarmSendStatus(post)
	a.addLatencyResult(post)
	a.addLatencySample(post)
	return nil
}

func (a *postLatencyPeriodSummaryAccumulator) addAlarmType(alarmType domain.AlarmType) error {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		a.summary.CommunityPostCount++
	case domain.AlarmTypeShorts:
		a.summary.ShortsPostCount++
	default:
		return fmt.Errorf("unsupported alarm type: %s", alarmType)
	}

	return nil
}

func (a *postLatencyPeriodSummaryAccumulator) addAlarmSendStatus(post PostSendCount) {
	if post.AlarmSentAt != nil {
		a.summary.AlarmSentPostCount++
	} else {
		a.summary.PendingPostCount++
	}
}

func (a *postLatencyPeriodSummaryAccumulator) addLatencyResult(post PostSendCount) {
	hasLatencyResult := post.AlarmLatencyMillis != nil || post.AlarmLatencyExceeded != nil
	if hasLatencyResult {
		a.summary.LatencyMeasuredPostCount++
	}

	a.addLatencyExceeded(post)
}

func (a *postLatencyPeriodSummaryAccumulator) addLatencyExceeded(post PostSendCount) {
	if post.AlarmLatencyExceeded == nil {
		return
	}

	if *post.AlarmLatencyExceeded {
		a.addExceededPost(post.AlarmType)
		return
	}
	a.summary.WithinTargetPostCount++
}

func (a *postLatencyPeriodSummaryAccumulator) addExceededPost(alarmType domain.AlarmType) {
	a.summary.ExceededPostCount++
	switch alarmType {
	case domain.AlarmTypeCommunity:
		a.summary.CommunityExceededPostCount++
	case domain.AlarmTypeShorts:
		a.summary.ShortsExceededPostCount++
	}
}

func (a *postLatencyPeriodSummaryAccumulator) addLatencySample(post PostSendCount) {
	if post.AlarmLatencyMillis != nil {
		latencyMillis := *post.AlarmLatencyMillis
		a.latencySumMillis += latencyMillis
		a.latencyMeasuredCount++
		a.latencySamplesMillis = append(a.latencySamplesMillis, latencyMillis)
		if a.latencyMeasuredCount == 1 || latencyMillis > a.maxLatencyMillis {
			a.maxLatencyMillis = latencyMillis
		}
	}
}

func (a postLatencyPeriodSummaryAccumulator) finalize() PostLatencyPeriodSummary {
	if a.latencyMeasuredCount <= 0 {
		return a.summary
	}

	averageLatencyMillis := a.latencySumMillis / a.latencyMeasuredCount
	p95LatencyMillis := discretePercentileMillis(a.latencySamplesMillis, 95, 100)
	maxLatencyMillis := a.maxLatencyMillis
	a.summary.AverageLatencyMillis = &averageLatencyMillis
	a.summary.P95LatencyMillis = p95LatencyMillis
	a.summary.MaxLatencyMillis = &maxLatencyMillis
	return a.summary
}

func discretePercentileMillis(samples []int64, numerator int, denominator int) *int64 {
	if len(samples) == 0 || numerator <= 0 || denominator <= 0 {
		return nil
	}

	sorted := append([]int64(nil), samples...)
	slices.Sort(sorted)

	rank := max((numerator*len(sorted)+denominator-1)/denominator, 1)
	rank = min(rank, len(sorted))
	value := sorted[rank-1]
	return &value
}

func normalizePostLatencyPeriods(periods []PostLatencyPeriod) ([]PostLatencyPeriod, error) {
	if len(periods) == 0 {
		return nil, nil
	}

	normalized := make([]PostLatencyPeriod, 0, len(periods))
	for i := range periods {
		period, err := normalizePostLatencyPeriod(periods[i], i)
		if err != nil {
			return nil, err
		}
		normalized = append(normalized, period)
	}

	return normalized, nil
}

func normalizePostLatencyPeriod(period PostLatencyPeriod, index int) (PostLatencyPeriod, error) {
	label := strings.TrimSpace(period.Label)
	if label == "" {
		return PostLatencyPeriod{}, fmt.Errorf("period at index %d: label is empty", index)
	}
	if period.StartAt.IsZero() {
		return PostLatencyPeriod{}, fmt.Errorf("period %q: start at is empty", label)
	}
	if period.EndAt.IsZero() {
		return PostLatencyPeriod{}, fmt.Errorf("period %q: end at is empty", label)
	}

	startAt := period.StartAt.UTC()
	endAt := period.EndAt.UTC()
	if !endAt.After(startAt) {
		return PostLatencyPeriod{}, fmt.Errorf("period %q: end at must be after start at", label)
	}

	return PostLatencyPeriod{
		Label:   label,
		StartAt: startAt,
		EndAt:   endAt,
	}, nil
}

func earliestPostLatencyPeriodStart(periods []PostLatencyPeriod) time.Time {
	earliest := periods[0].StartAt
	for i := 1; i < len(periods); i++ {
		if periods[i].StartAt.Before(earliest) {
			earliest = periods[i].StartAt
		}
	}
	return earliest
}

func postLatencyObservedAt(post PostSendCount) (time.Time, error) {
	if post.ActualPublishedAt != nil {
		return post.ActualPublishedAt.UTC(), nil
	}
	if post.DetectedAt != nil {
		return post.DetectedAt.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("observed at is empty")
}

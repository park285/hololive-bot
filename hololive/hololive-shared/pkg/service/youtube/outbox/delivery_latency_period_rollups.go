package outbox

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

	accumulators := make([]postLatencyPeriodSummaryAccumulator, len(normalizedPeriods))
	for i := range normalizedPeriods {
		accumulators[i].summary = PostLatencyPeriodSummary{
			Label:   normalizedPeriods[i].Label,
			StartAt: normalizedPeriods[i].StartAt,
			EndAt:   normalizedPeriods[i].EndAt,
		}
	}

	for i := range posts {
		observedAt, err := postLatencyObservedAt(posts[i])
		if err != nil {
			return nil, fmt.Errorf("post[%d] %s: %w", i, strings.TrimSpace(posts[i].ContentID), err)
		}

		for j := range normalizedPeriods {
			if observedAt.Before(normalizedPeriods[j].StartAt) || !observedAt.Before(normalizedPeriods[j].EndAt) {
				continue
			}
			if err := accumulators[j].add(posts[i]); err != nil {
				return nil, fmt.Errorf("post[%d] %s: %w", i, strings.TrimSpace(posts[i].ContentID), err)
			}
		}
	}

	summaries := make([]PostLatencyPeriodSummary, 0, len(accumulators))
	for i := range accumulators {
		summaries = append(summaries, accumulators[i].finalize())
	}

	return summaries, nil
}

type postLatencyPeriodSummaryAccumulator struct {
	summary              PostLatencyPeriodSummary
	latencySumMillis     int64
	latencyMeasuredCount int64
	latencySamplesMillis []int64
	maxLatencyMillis     int64
}

func (a *postLatencyPeriodSummaryAccumulator) add(post PostSendCount) error {
	a.summary.TotalPostCount++

	switch post.AlarmType {
	case domain.AlarmTypeCommunity:
		a.summary.CommunityPostCount++
	case domain.AlarmTypeShorts:
		a.summary.ShortsPostCount++
	default:
		return fmt.Errorf("unsupported alarm type: %s", post.AlarmType)
	}

	if post.AlarmSentAt != nil {
		a.summary.AlarmSentPostCount++
	} else {
		a.summary.PendingPostCount++
	}

	hasLatencyResult := post.AlarmLatencyMillis != nil || post.AlarmLatencyExceeded != nil
	if hasLatencyResult {
		a.summary.LatencyMeasuredPostCount++
	}

	if post.AlarmLatencyExceeded != nil {
		if *post.AlarmLatencyExceeded {
			a.summary.ExceededPostCount++
			switch post.AlarmType {
			case domain.AlarmTypeCommunity:
				a.summary.CommunityExceededPostCount++
			case domain.AlarmTypeShorts:
				a.summary.ShortsExceededPostCount++
			}
		} else {
			a.summary.WithinTargetPostCount++
		}
	}

	if post.AlarmLatencyMillis != nil {
		latencyMillis := *post.AlarmLatencyMillis
		a.latencySumMillis += latencyMillis
		a.latencyMeasuredCount++
		a.latencySamplesMillis = append(a.latencySamplesMillis, latencyMillis)
		if a.latencyMeasuredCount == 1 || latencyMillis > a.maxLatencyMillis {
			a.maxLatencyMillis = latencyMillis
		}
	}

	return nil
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
		label := strings.TrimSpace(periods[i].Label)
		if label == "" {
			return nil, fmt.Errorf("period at index %d: label is empty", i)
		}
		if periods[i].StartAt.IsZero() {
			return nil, fmt.Errorf("period %q: start at is empty", label)
		}
		if periods[i].EndAt.IsZero() {
			return nil, fmt.Errorf("period %q: end at is empty", label)
		}

		startAt := periods[i].StartAt.UTC()
		endAt := periods[i].EndAt.UTC()
		if !endAt.After(startAt) {
			return nil, fmt.Errorf("period %q: end at must be after start at", label)
		}

		normalized = append(normalized, PostLatencyPeriod{
			Label:   label,
			StartAt: startAt,
			EndAt:   endAt,
		})
	}

	return normalized, nil
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

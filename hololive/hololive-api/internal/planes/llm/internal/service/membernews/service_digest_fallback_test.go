package membernews

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/newsmodel"
	"github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
)

type failingDigestSummarizer struct{ err error }

func (f failingDigestSummarizer) Summarize(context.Context, *newsmodel.SummarizeInput) (*model.Digest, error) {
	return nil, f.err
}

func digestFallbackCandidates() []newsmodel.FilteredCandidate {
	return []newsmodel.FilteredCandidate{{
		Candidate:     newsmodel.Candidate{Title: "EXPO"},
		EffectiveDate: time.Date(2026, time.February, 20, 10, 0, 0, 0, time.UTC),
		MemberText:    "사쿠라 미코",
		Category:      newsmodel.CategoryEvent,
		SourceURL:     "https://hololive.hololivepro.com/news/1",
	}}
}

func TestSummarizeRoomDigestOwnsFallbackAndRecordsReason(t *testing.T) {
	service := NewService(nil, failingDigestSummarizer{err: errors.New("llm down")}, nil, nil, nil)
	before := memberNewsDigestResultCount(t, "fallback", "summarizer_error")

	digest, err := service.summarizeRoomDigest(t.Context(), "room-1", model.PeriodWeekly, []string{"사쿠라 미코"}, digestFallbackCandidates(), time.Now())
	if err != nil {
		t.Fatalf("summarizeRoomDigest() error = %v", err)
	}

	if digest == nil || digest.ResultType != sharedmodel.SummaryResultFallback || len(digest.TopItems) != 1 {
		t.Fatalf("digest = %#v, want deterministic fallback digest", digest)
	}

	if after := memberNewsDigestResultCount(t, "fallback", "summarizer_error"); after != before+1 {
		t.Fatalf("hololive_member_news_digest_result_total{result_type=fallback,reason=summarizer_error} = %v, want %v", after, before+1)
	}
}

func memberNewsDigestResultCount(t *testing.T, resultType, reason string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() != "hololive_member_news_digest_result_total" {
			continue
		}

		for _, metric := range family.GetMetric() {
			labels := map[string]string{}

			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}

			if labels["result_type"] == resultType && labels["reason"] == reason {
				return metric.GetCounter().GetValue()
			}
		}
	}

	return 0
}

type countingDigestSummarizer struct{ calls int }

func (c *countingDigestSummarizer) Summarize(context.Context, *newsmodel.SummarizeInput) (*model.Digest, error) {
	c.calls++

	return nil, errors.New("unexpected summarize call")
}

// 검증된 후보가 0건이면 요약기를 부르지 않고 result_type=empty digest를 metric에 남긴다.
func TestGeneratePreparedRoomDigestRecordsEmptyResult(t *testing.T) {
	summarizer := &countingDigestSummarizer{}
	service := NewService(nil, summarizer, nil, nil, nil)
	now := time.Date(2026, time.February, 20, 10, 0, 0, 0, time.UTC)
	before := memberNewsDigestResultCount(t, "empty", "")

	digest, err := service.generatePreparedRoomDigest(t.Context(), "room-1", model.PeriodWeekly, now, []string{"사쿠라 미코"}, filter.PrepareCandidates(nil, model.PeriodWeekly, now))
	if err != nil {
		t.Fatalf("generatePreparedRoomDigest() error = %v", err)
	}

	if digest == nil || digest.ResultType != sharedmodel.SummaryResultEmpty || len(digest.TopItems) != 0 {
		t.Fatalf("digest = %#v, want empty digest", digest)
	}

	if summarizer.calls != 0 {
		t.Fatalf("summarizer calls = %d, want 0", summarizer.calls)
	}

	if after := memberNewsDigestResultCount(t, "empty", ""); after != before+1 {
		t.Fatalf("hololive_member_news_digest_result_total{result_type=empty,reason=\"\"} = %v, want %v", after, before+1)
	}
}

// 호출자 취소는 fallback digest로 숨기지 않는다.
func TestSummarizeRoomDigestPropagatesCallerCancellation(t *testing.T) {
	service := NewService(nil, failingDigestSummarizer{err: context.Canceled}, nil, nil, nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	digest, err := service.summarizeRoomDigest(ctx, "room-1", model.PeriodWeekly, []string{"사쿠라 미코"}, digestFallbackCandidates(), time.Now())
	if !errors.Is(err, context.Canceled) || digest != nil {
		t.Fatalf("summarizeRoomDigest() = (%#v, %v), want (nil, context.Canceled)", digest, err)
	}
}

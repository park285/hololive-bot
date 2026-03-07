// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package membernews

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNormalizePeriodAndDefaultHeadlineWrappers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		input           Period
		wantPeriod      Period
		wantDefaultHead string
	}{
		{
			name:            "monthly keyword",
			input:           Period("monthly"),
			wantPeriod:      PeriodMonthly,
			wantDefaultHead: "📅 이번달 구독 멤버 뉴스",
		},
		{
			name:            "korean monthly keyword",
			input:           Period("이번달"),
			wantPeriod:      PeriodMonthly,
			wantDefaultHead: "📅 이번달 구독 멤버 뉴스",
		},
		{
			name:            "unknown defaults to weekly",
			input:           Period("unknown"),
			wantPeriod:      PeriodWeekly,
			wantDefaultHead: "🗞️ 이번주 구독 멤버 뉴스",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizePeriod(tc.input); got != tc.wantPeriod {
				t.Fatalf("NormalizePeriod() = %q, want %q", got, tc.wantPeriod)
			}
			if got := defaultHeadline(tc.input); got != tc.wantDefaultHead {
				t.Fatalf("defaultHeadline() = %q, want %q", got, tc.wantDefaultHead)
			}
		})
	}
}

func TestFilterCandidates_TypedNilValidatorIsTreatedAsNil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 5, 10, 0, 0, 0, time.FixedZone("KST", 9*60*60))
	eventTime := now
	candidates := []Candidate{
		{
			ID:             1,
			Type:           domain.MajorEventTypeNews,
			Title:          "Miko Birthday Live notice",
			Description:    "official update",
			Members:        []string{"Miko"},
			EventStartDate: &eventTime,
			SourceURL:      "https://example.com/news/1",
		},
	}

	var typedNilValidator *SourceValidator
	got := FilterCandidates(
		candidates,
		PeriodWeekly,
		now,
		[]string{"Miko"},
		nil,
		typedNilValidator,
	)

	if len(got) != 1 {
		t.Fatalf("FilterCandidates() len = %d, want 1 (typed-nil validator must behave as nil)", len(got))
	}
	if got[0].SourceTier != SourceTierCommunity {
		t.Fatalf("FilterCandidates() source tier = %q, want %q", got[0].SourceTier, SourceTierCommunity)
	}
}

func TestSchedulerWrappers_ReturnInstanceOnNilDependencies(t *testing.T) {
	t.Parallel()

	weekly := NewScheduler(nil, nil, nil, nil, nil)
	if weekly == nil {
		t.Fatalf("NewScheduler() returned nil")
	}
	if err := weekly.SendWeeklyDigest(context.Background()); err == nil || !strings.Contains(err.Error(), "member news service is nil") {
		t.Fatalf("SendWeeklyDigest() error = %v, want member news service is nil", err)
	}

	monthly := NewMonthlyScheduler(nil, nil, nil, nil, nil)
	if monthly == nil {
		t.Fatalf("NewMonthlyScheduler() returned nil")
	}
	if err := monthly.SendMonthlyDigest(context.Background()); err == nil || !strings.Contains(err.Error(), "member news service is nil") {
		t.Fatalf("SendMonthlyDigest() error = %v, want member news service is nil", err)
	}
}

func TestSummarizerWrappers_BasicContracts(t *testing.T) {
	t.Parallel()

	sum := NewSummarizer(nil, nil, nil, nil)
	if sum == nil {
		t.Fatalf("NewSummarizer() returned nil")
	}

	candidates := []FilteredCandidate{
		{
			Candidate: Candidate{
				Title:       "Aqua concert notice",
				Description: "concert details",
				SourceURL:   "https://example.com/aqua",
			},
			EffectiveDate: time.Date(2026, 3, 5, 9, 0, 0, 0, time.FixedZone("KST", 9*60*60)),
			MemberText:    "Aqua",
			Category:      CategoryEvent,
			SourceTier:    SourceTierOfficial,
			SourceURL:     "https://example.com/aqua",
		},
	}

	digest, err := sum.Summarize(context.Background(), SummarizeInput{
		Period:     PeriodWeekly,
		Candidates: candidates,
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if digest == nil {
		t.Fatalf("Summarize() returned nil digest")
	}
	if len(digest.TopItems) == 0 {
		t.Fatalf("Summarize() returned empty top items for fallback path")
	}

	consensusSummarizer := NewConsensusSummarizer(
		fakePrimarySummarizer{digest: digest},
		nil,
		nil,
		nil,
		consensus.Config{},
		nil,
	)
	if consensusSummarizer == nil {
		t.Fatalf("NewConsensusSummarizer() returned nil")
	}

	got, err := consensusSummarizer.Summarize(context.Background(), SummarizeInput{Period: PeriodWeekly})
	if err != nil {
		t.Fatalf("ConsensusSummarize() error = %v", err)
	}
	if got == nil {
		t.Fatalf("ConsensusSummarize() returned nil digest")
	}
}

func TestBuildDeterministicFallbackWrapper(t *testing.T) {
	t.Parallel()

	candidates := []FilteredCandidate{
		{
			Candidate: Candidate{
				Title:       "Marine goods release",
				Description: "new merchandise launched",
				SourceURL:   "https://example.com/marine",
			},
			EffectiveDate: time.Date(2026, 3, 1, 9, 0, 0, 0, time.FixedZone("KST", 9*60*60)),
			MemberText:    "Marine",
			Category:      CategoryGoods,
			SourceTier:    SourceTierOfficial,
			SourceURL:     "https://example.com/marine",
		},
	}

	got := BuildDeterministicFallback(PeriodMonthly, candidates)
	if got == nil {
		t.Fatalf("BuildDeterministicFallback() returned nil")
	}
	if got.Period != PeriodMonthly {
		t.Fatalf("BuildDeterministicFallback() period = %q, want %q", got.Period, PeriodMonthly)
	}
	if got.Headline != "📅 이번달 구독 멤버 뉴스" {
		t.Fatalf("BuildDeterministicFallback() headline = %q", got.Headline)
	}
	if got.TotalCount != len(candidates) {
		t.Fatalf("BuildDeterministicFallback() total_count = %d, want %d", got.TotalCount, len(candidates))
	}
}

type fakePrimarySummarizer struct {
	digest *Digest
	err    error
}

func (f fakePrimarySummarizer) Summarize(_ context.Context, _ SummarizeInput) (*Digest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.digest, nil
}

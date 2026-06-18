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

package summarizer

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNeedsSummaryAdjudication(t *testing.T) {
	tests := []struct {
		name      string
		verdict   *consensus.ReviewVerdict
		threshold float64
		want      bool
	}{
		{
			name:      "nil verdict",
			verdict:   nil,
			threshold: 0.85,
			want:      false,
		},
		{
			name: "not approved",
			verdict: &consensus.ReviewVerdict{
				Approved:   false,
				Confidence: 0.99,
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "critical issue",
			verdict: &consensus.ReviewVerdict{
				Approved:   true,
				Confidence: 0.99,
				Issues: []consensus.ReviewIssue{
					{Severity: "critical"},
				},
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "low confidence",
			verdict: &consensus.ReviewVerdict{
				Approved:   true,
				Confidence: 0.7,
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "approved and confident",
			verdict: &consensus.ReviewVerdict{
				Approved:   true,
				Confidence: 0.92,
			},
			threshold: 0.85,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := consensus.NeedsAdjudication(tt.verdict, tt.threshold)
			if got != tt.want {
				t.Fatalf("NeedsAdjudication() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeSummarySeverity(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "critical", want: "critical"},
		{in: " WARNING ", want: "warning"},
		{in: "Info", want: "info"},
		{in: "unknown", want: "info"},
		{in: "", want: "info"},
	}

	for _, tt := range tests {
		got := consensus.NormalizeSeverity(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestShouldRunConsensusReview_SingleHighlightRunsReview(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{{Name: "fes"}},
	}

	if !shouldRunConsensusReview(resp) {
		t.Fatal("shouldRunConsensusReview() = false, want true for single highlight")
	}
}

func TestDeriveConsensusBudget_CapsToParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent deadline missing")
	}

	child, childCancel, ok := deriveConsensusBudget(parent, 10*time.Second)
	if !ok {
		t.Fatal("deriveConsensusBudget() ok = false, want true")
	}
	defer childCancel()

	childDeadline, ok := child.Deadline()
	if !ok {
		t.Fatal("child deadline missing")
	}

	gap := parentDeadline.Sub(childDeadline)
	if gap < 150*time.Millisecond || gap > 500*time.Millisecond {
		t.Fatalf("deadline gap = %v, want around 250ms reserve", gap)
	}
}

func TestDeriveConsensusBudget_ReturnsFalseWhenNoBudgetLeft(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	child, childCancel, ok := deriveConsensusBudget(parent, 10*time.Second)
	if ok {
		t.Fatal("deriveConsensusBudget() ok = true, want false")
	}
	if child != nil {
		t.Fatalf("deriveConsensusBudget() child = %v, want nil", child)
	}
	if childCancel != nil {
		t.Fatal("deriveConsensusBudget() cancel should be nil when budget is exhausted")
	}
}

func TestEventSummarizer_RunConsensus_UsesReservedParentBudgetForReview(t *testing.T) {
	reviewer := &deadlineCapturingSummarizer{
		jsonResponse: `{"approved":true,"confidence":0.99,"issues":[]}`,
	}
	summarizer := NewEventSummarizer(
		nil,
		nil,
		nil,
		testLogger(),
		WithSummarizerConsensus(reviewer, nil, SummarizerConsensusConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		}),
	)

	parent, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent deadline missing")
	}

	primary := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "fes"},
			{Name: "expo"},
		},
	}
	result, used := summarizer.runConsensus(
		parent,
		[]domain.MajorEvent{{ID: 1, Title: "fes"}},
		SummaryTypeWeekly,
		"2026-02-15",
		"",
		primary,
	)

	if reviewer.callCount != 1 {
		t.Fatalf("reviewer callCount = %d, want 1", reviewer.callCount)
	}
	if result != primary {
		t.Fatal("runConsensus() should keep primary summary when review passes")
	}
	if used {
		t.Fatal("runConsensus() used = true, want false")
	}
	deadlineGap := parentDeadline.Sub(reviewer.deadline)
	if deadlineGap < 150*time.Millisecond || deadlineGap > 500*time.Millisecond {
		t.Fatalf("reviewer deadline gap = %v, want around 250ms reserve", deadlineGap)
	}
}

func TestEventSummarizer_RunConsensus_SkipsWhenReviewBudgetExhausted(t *testing.T) {
	reviewer := &mockSummarizer{
		jsonResponse: `{"approved":true,"confidence":0.99,"issues":[]}`,
	}
	summarizer := NewEventSummarizer(
		nil,
		nil,
		nil,
		testLogger(),
		WithSummarizerConsensus(reviewer, nil, SummarizerConsensusConfig{
			Enabled:             true,
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		}),
	)

	parent, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	primary := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "fes"},
			{Name: "expo"},
		},
	}
	result, used := summarizer.runConsensus(
		parent,
		[]domain.MajorEvent{{ID: 1, Title: "fes"}},
		SummaryTypeWeekly,
		"2026-02-15",
		"",
		primary,
	)

	if reviewer.callCount != 0 {
		t.Fatalf("reviewer callCount = %d, want 0", reviewer.callCount)
	}
	if result != primary {
		t.Fatal("runConsensus() should keep primary summary when budget is exhausted")
	}
	if used {
		t.Fatal("runConsensus() used = true, want false")
	}
}

type deadlineCapturingSummarizer struct {
	jsonResponse string
	err          error
	callCount    int
	deadline     time.Time
}

func (m *deadlineCapturingSummarizer) GenerateJSON(ctx context.Context, _, _ string, _ map[string]any) (string, error) {
	m.callCount++
	deadline, ok := ctx.Deadline()
	if ok {
		m.deadline = deadline
	}
	return m.jsonResponse, m.err
}

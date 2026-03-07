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
	"testing"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
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

package summarizer

import "testing"

func TestNeedsSummaryAdjudication(t *testing.T) {
	tests := []struct {
		name      string
		verdict   *summaryReviewVerdict
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
			verdict: &summaryReviewVerdict{
				Approved:   false,
				Confidence: 0.99,
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "critical issue",
			verdict: &summaryReviewVerdict{
				Approved:   true,
				Confidence: 0.99,
				Issues: []summaryReviewIssue{
					{Severity: "critical"},
				},
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "low confidence",
			verdict: &summaryReviewVerdict{
				Approved:   true,
				Confidence: 0.7,
			},
			threshold: 0.85,
			want:      true,
		},
		{
			name: "approved and confident",
			verdict: &summaryReviewVerdict{
				Approved:   true,
				Confidence: 0.92,
			},
			threshold: 0.85,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsSummaryAdjudication(tt.verdict, tt.threshold)
			if got != tt.want {
				t.Fatalf("needsSummaryAdjudication() = %v, want %v", got, tt.want)
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
		got := normalizeSeverity(tt.in)
		if got != tt.want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

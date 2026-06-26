package consensus

import "testing"

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"critical", "critical"},
		{"warning", "warning"},
		{"info", "info"},
		{"CRITICAL", "critical"},
		{"Warning", "warning"},
		{"INFO", "info"},
		{"  critical  ", "critical"},
		{"\tWARNING\n", "warning"},
		{"", "info"},
		{"   ", "info"},
		{"unknown", "info"},
		{"high", "info"},
		{"criticalish", "info"},
		{"crit", "info"},
	}

	for _, c := range cases {
		if got := NormalizeSeverity(c.in); got != c.want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHasCriticalIssues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		issues []ReviewIssue
		want   bool
	}{
		{"nil", nil, false},
		{"empty", []ReviewIssue{}, false},
		{"single critical", []ReviewIssue{{Severity: "critical"}}, true},
		{"single warning", []ReviewIssue{{Severity: "warning"}}, false},
		{"single info", []ReviewIssue{{Severity: "info"}}, false},
		{"critical among others", []ReviewIssue{{Severity: "info"}, {Severity: "critical"}, {Severity: "warning"}}, true},
		{"none critical", []ReviewIssue{{Severity: "warning"}, {Severity: "info"}}, false},
		{"uppercase critical not matched", []ReviewIssue{{Severity: "CRITICAL"}}, false},
		{"empty severity", []ReviewIssue{{Severity: ""}}, false},
	}

	for _, c := range cases {
		if got := HasCriticalIssues(c.issues); got != c.want {
			t.Errorf("HasCriticalIssues(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNeedsAdjudication(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		verdict   *ReviewVerdict
		threshold float64
		want      bool
	}{
		{"nil verdict", nil, 0.85, false},
		{
			"not approved",
			&ReviewVerdict{Approved: false, Confidence: 1.0},
			0.85,
			true,
		},
		{
			"approved with critical issue",
			&ReviewVerdict{Approved: true, Confidence: 1.0, Issues: []ReviewIssue{{Severity: "critical"}}},
			0.85,
			true,
		},
		{
			"approved high confidence no critical",
			&ReviewVerdict{Approved: true, Confidence: 0.9, Issues: []ReviewIssue{{Severity: "warning"}}},
			0.85,
			false,
		},
		{
			"approved confidence below threshold",
			&ReviewVerdict{Approved: true, Confidence: 0.5},
			0.85,
			true,
		},
		{
			"approved confidence equal threshold",
			&ReviewVerdict{Approved: true, Confidence: 0.85},
			0.85,
			false,
		},
		{
			"approved confidence just below threshold",
			&ReviewVerdict{Approved: true, Confidence: 0.8499},
			0.85,
			true,
		},
		{
			"not approved overrides critical and confidence",
			&ReviewVerdict{Approved: false, Confidence: 0.0, Issues: []ReviewIssue{{Severity: "critical"}}},
			0.85,
			true,
		},
		{
			"approved no issues confidence above threshold",
			&ReviewVerdict{Approved: true, Confidence: 1.0},
			0.85,
			false,
		},
		{
			"threshold zero approved zero confidence",
			&ReviewVerdict{Approved: true, Confidence: 0.0},
			0.0,
			false,
		},
		{
			"approved critical takes priority over confidence pass",
			&ReviewVerdict{Approved: true, Confidence: 1.0, Issues: []ReviewIssue{{Severity: "info"}, {Severity: "critical"}}},
			0.85,
			true,
		},
	}

	for _, c := range cases {
		if got := NeedsAdjudication(c.verdict, c.threshold); got != c.want {
			t.Errorf("NeedsAdjudication(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

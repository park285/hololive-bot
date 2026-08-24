package consensus

import (
	"context"
	"testing"
	"time"
)

func TestStageContextCapsToParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()

	parentDeadline, ok := parent.Deadline()
	if !ok {
		t.Fatal("parent deadline missing")
	}

	child, childCancel, ok := StageContext(parent, 10*time.Second)
	if !ok {
		t.Fatal("StageContext() ok = false, want true")
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

func TestStageContextReturnsFalseWhenNoBudgetLeft(t *testing.T) {
	parent, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()

	child, childCancel, ok := StageContext(parent, 10*time.Second)
	if ok {
		t.Fatal("StageContext() ok = true, want false")
	}

	if child != nil {
		t.Fatalf("StageContext() child = %v, want nil", child)
	}

	if childCancel != nil {
		t.Fatal("StageContext() cancel should be nil when budget is exhausted")
	}
}

func TestNormalizeSeverity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{testSeverityCritical, testSeverityCritical},
		{testSeverityWarning, testSeverityWarning},
		{testSeverityInfo, testSeverityInfo},
		{"CRITICAL", testSeverityCritical},
		{"Warning", testSeverityWarning},
		{"INFO", testSeverityInfo},
		{"  critical  ", testSeverityCritical},
		{"\tWARNING\n", testSeverityWarning},
		{"", testSeverityInfo},
		{"   ", testSeverityInfo},
		{"unknown", testSeverityInfo},
		{"high", testSeverityInfo},
		{"criticalish", testSeverityInfo},
		{"crit", testSeverityInfo},
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
		{"single critical", []ReviewIssue{{Severity: testSeverityCritical}}, true},
		{"single warning", []ReviewIssue{{Severity: testSeverityWarning}}, false},
		{"single info", []ReviewIssue{{Severity: testSeverityInfo}}, false},
		{"critical among others", []ReviewIssue{{Severity: testSeverityInfo}, {Severity: testSeverityCritical}, {Severity: testSeverityWarning}}, true},
		{"none critical", []ReviewIssue{{Severity: testSeverityWarning}, {Severity: testSeverityInfo}}, false},
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
			&ReviewVerdict{Approved: true, Confidence: 1.0, Issues: []ReviewIssue{{Severity: testSeverityCritical}}},
			0.85,
			true,
		},
		{
			"approved high confidence no critical",
			&ReviewVerdict{Approved: true, Confidence: 0.9, Issues: []ReviewIssue{{Severity: testSeverityWarning}}},
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
			&ReviewVerdict{Approved: false, Confidence: 0.0, Issues: []ReviewIssue{{Severity: testSeverityCritical}}},
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
			&ReviewVerdict{Approved: true, Confidence: 1.0, Issues: []ReviewIssue{{Severity: testSeverityInfo}, {Severity: testSeverityCritical}}},
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

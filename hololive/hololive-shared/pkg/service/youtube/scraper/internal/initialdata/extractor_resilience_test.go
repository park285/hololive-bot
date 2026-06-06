package initialdata

import (
	"errors"
	"testing"
)

func TestExtractSupportsWindowDotYtInitialData(t *testing.T) {
	got, err := Extract(`<script>window.ytInitialData = {"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[]}}};</script>`)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if got == "" || got[0] != '{' {
		t.Fatalf("unexpected extracted payload: %q", got)
	}
}

func TestPickBestYtInitialDataCandidateSkipsInvalidJSON(t *testing.T) {
	got, ok := pickBestYtInitialDataCandidate([]string{
		`{invalid`,
		`{"responseContext":{},"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[]}}}`,
	})
	if !ok {
		t.Fatal("expected valid candidate")
	}
	if got == "{invalid" {
		t.Fatal("invalid candidate selected")
	}
}

func TestFindNextAnchorCandidateDoesNotScanUnboundedAssignmentGap(t *testing.T) {
	html := "var ytInitialData" + string(make([]byte, maxYtInitialDataAssignmentScanBytes+8)) + `={"contents":{}};`
	if _, _, ok := findNextAnchorCandidate(html, "var ytInitialData", 0); ok {
		t.Fatal("candidate should not be found across unbounded assignment gap")
	}
}

func TestPickBestYtInitialDataCandidateRejectsAllInvalidJSON(t *testing.T) {
	got, ok := pickBestYtInitialDataCandidate([]string{
		`{invalid`,
		`{also bad and much much longer than the first candidate`,
	})
	if ok {
		t.Fatalf("all-invalid candidates must not be returned as success, got %q", got)
	}
	if got != "" {
		t.Fatalf("got = %q, want empty string", got)
	}
}

func TestExtractReturnsNotFoundWhenAllCandidatesInvalid(t *testing.T) {
	_, err := Extract(`<script>var ytInitialData = {"x":NaN,};</script>`)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Extract() error = %v, want ErrNotFound for parser-drift visibility", err)
	}
}

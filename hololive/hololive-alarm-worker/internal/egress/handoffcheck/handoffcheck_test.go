package handoffcheck

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
)

const testRequestID = "iris-request-1"

func snapshot(state string) *iris.ReplyStatusSnapshot {
	return &iris.ReplyStatusSnapshot{RequestID: testRequestID, State: state}
}

func TestDecideRoutesEveryObservation(t *testing.T) {
	const horizon = 60 * time.Second

	notFound := fmt.Errorf("get reply status: %w", &iris.HTTPError{StatusCode: 404})
	unavailable := fmt.Errorf("get reply status: %w", &iris.HTTPError{StatusCode: 503})
	transport := errors.New("dial tcp: connection refused")

	cases := []struct {
		name       string
		in         Input
		wantAction Action
		wantCode   string
	}{
		{"handoff completed", Input{Snapshot: snapshot("handoff_completed")}, ActionComplete, ""},
		{"completed state is normalized", Input{Snapshot: snapshot("  HANDOFF_COMPLETED ")}, ActionComplete, ""},
		{"failed with generation and budget left", Input{Snapshot: snapshot("failed"), Generation: 1, RetryBudgetLeft: true}, ActionReissue, CodeFailed},
		{"failed at the last generation", Input{Snapshot: snapshot("failed"), Generation: iris.ReplyReissueMaxGenerations, RetryBudgetLeft: true}, ActionExhausted, CodeFailed},
		{"failed without attempt budget", Input{Snapshot: snapshot("failed"), RetryBudgetLeft: false}, ActionExhausted, CodeFailed},
		{"outcome unknown", Input{Snapshot: snapshot("outcome_unknown"), RetryBudgetLeft: true}, ActionQuarantine, CodeOutcomeUnknown},
		{"unrecognized state", Input{Snapshot: snapshot("delivered")}, ActionQuarantine, CodeOutcomeUnknown},
		{"empty state", Input{Snapshot: snapshot("")}, ActionQuarantine, CodeOutcomeUnknown},
		{"nil snapshot", Input{}, ActionQuarantine, CodeOutcomeUnknown},
		{"request id mismatch", Input{Snapshot: &iris.ReplyStatusSnapshot{RequestID: "other", State: "handoff_completed"}}, ActionQuarantine, CodeOutcomeUnknown},
		{"status not found", Input{QueryErr: notFound}, ActionQuarantine, CodeStatusMissing},
		{"pending before horizon", Input{Snapshot: snapshot("queued"), Elapsed: horizon - time.Millisecond}, ActionReschedule, ""},
		{"sending before horizon", Input{Snapshot: snapshot("sending"), Elapsed: 5 * time.Second}, ActionReschedule, ""},
		{"pending at horizon", Input{Snapshot: snapshot("prepared"), Elapsed: horizon}, ActionQuarantine, CodeUnconfirmed},
		{"server error before horizon", Input{QueryErr: unavailable, Elapsed: 20 * time.Second}, ActionReschedule, ""},
		{"transport error before horizon", Input{QueryErr: transport, Elapsed: 20 * time.Second}, ActionReschedule, ""},
		{"query error at horizon", Input{QueryErr: unavailable, Elapsed: horizon}, ActionQuarantine, CodeUnconfirmed},
		{"not found beats pending horizon", Input{QueryErr: notFound, Elapsed: time.Second}, ActionQuarantine, CodeStatusMissing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.RequestID = testRequestID
			tc.in.Horizon = horizon

			got := Decide(tc.in)
			if got.Action != tc.wantAction || got.Code != tc.wantCode {
				t.Fatalf("Decide() = %+v, want action %d code %q", got, tc.wantAction, tc.wantCode)
			}

			if (got.Action == ActionReschedule || got.Action == ActionComplete) != (got.Detail == "") {
				t.Fatalf("Decide() detail = %q, want detail only for transitions that record an error", got.Detail)
			}
		})
	}
}

func TestDecideKeepsRequestIDOutOfStoredQueryErrorDetail(t *testing.T) {
	err := fmt.Errorf("GET /reply-status/%s: %w", testRequestID, &iris.HTTPError{StatusCode: 503, URL: "/reply-status/" + testRequestID})

	got := Decide(Input{RequestID: testRequestID, QueryErr: err, Elapsed: time.Minute, Horizon: time.Minute})
	if got.Action != ActionQuarantine || got.Detail != "iris handoff unconfirmed at horizon: iris reply status query failed with http 503" {
		t.Fatalf("Decide() = %+v, want horizon quarantine with only the HTTP status", got)
	}
}

func TestNextStepFollowsLadderFromAdmission(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    time.Duration
	}{
		{0, 15 * time.Second},
		{5 * time.Second, 15 * time.Second},
		{19 * time.Second, 15 * time.Second},
		{20 * time.Second, time.Minute},
		{79 * time.Second, time.Minute},
		{80 * time.Second, 5 * time.Minute},
		{400 * time.Second, 5 * time.Minute},
		{time.Hour, 5 * time.Minute},
	}

	for _, tc := range cases {
		if got := NextStep(tc.elapsed); got != tc.want {
			t.Errorf("NextStep(%s) = %s, want %s", tc.elapsed, got, tc.want)
		}
	}
}

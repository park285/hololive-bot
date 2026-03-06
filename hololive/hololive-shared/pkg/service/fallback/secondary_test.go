package fallback

import (
	"context"
	"errors"
	"testing"
)

func TestRunSecondary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		plan        SecondaryPlan
		wantOutcome string
		wantErr     bool
	}{
		{
			name: "skipped when policy says no",
			plan: SecondaryPlan{
				Service:   "svc",
				Operation: "op",
				Trigger:   TriggerOnFailures,
				ShouldRun: false,
			},
			wantOutcome: "skipped",
		},
		{
			name: "blocked before run",
			plan: SecondaryPlan{
				Service:   "svc",
				Operation: "op",
				Trigger:   TriggerOnFailures,
				ShouldRun: true,
				Blocked:   true,
			},
			wantOutcome: "blocked",
		},
		{
			name: "hit when successes and items exist",
			plan: SecondaryPlan{
				Service:   "svc",
				Operation: "op",
				Trigger:   TriggerOnFailures,
				ShouldRun: true,
				Run: func(context.Context) (SecondaryResult, error) {
					return SecondaryResult{Items: 2, Successes: 1}, nil
				},
			},
			wantOutcome: "hit",
		},
		{
			name: "miss when successful but empty",
			plan: SecondaryPlan{
				Service:   "svc",
				Operation: "op",
				Trigger:   TriggerOnFailures,
				ShouldRun: true,
				Run: func(context.Context) (SecondaryResult, error) {
					return SecondaryResult{Items: 0, Successes: 1}, nil
				},
			},
			wantOutcome: "miss",
		},
		{
			name: "error when runner errors",
			plan: SecondaryPlan{
				Service:   "svc",
				Operation: "op",
				Trigger:   TriggerOnFailures,
				ShouldRun: true,
				Run: func(context.Context) (SecondaryResult, error) {
					return SecondaryResult{}, errors.New("boom")
				},
			},
			wantOutcome: "error",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := RunSecondary(context.Background(), tt.plan)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RunSecondary() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Outcome != tt.wantOutcome {
				t.Fatalf("RunSecondary() outcome = %q, want %q", got.Outcome, tt.wantOutcome)
			}
		})
	}
}

func TestSecondaryOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result SecondaryResult
		want   string
	}{
		{name: "error when no successes", result: SecondaryResult{}, want: "error"},
		{name: "hit when items exist", result: SecondaryResult{Items: 1, Successes: 1}, want: "hit"},
		{name: "miss when empty success", result: SecondaryResult{Items: 0, Successes: 2}, want: "miss"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := secondaryOutcome(tt.result); got != tt.want {
				t.Fatalf("secondaryOutcome() = %q, want %q", got, tt.want)
			}
		})
	}
}

package fallback

import (
	"context"
	"fmt"
)

type SecondaryPlan struct {
	Service   string
	Operation string
	Trigger   Trigger
	ShouldRun bool
	Blocked   bool
	Run       func(context.Context) (SecondaryResult, error)
}

type SecondaryResult struct {
	Items     int
	Successes int
}

type SecondaryExecution struct {
	Outcome string
	Result  SecondaryResult
}

func RunSecondary(ctx context.Context, plan SecondaryPlan) (SecondaryExecution, error) {
	if !plan.ShouldRun {
		ObserveExecution(plan.Service, plan.Operation, plan.Trigger, "skipped")
		return SecondaryExecution{Outcome: "skipped"}, nil
	}
	if plan.Blocked {
		ObserveExecution(plan.Service, plan.Operation, plan.Trigger, "blocked")
		return SecondaryExecution{Outcome: "blocked"}, nil
	}
	if plan.Run == nil {
		ObserveExecution(plan.Service, plan.Operation, plan.Trigger, "error")
		return SecondaryExecution{Outcome: "error"}, fmt.Errorf("run secondary fallback: missing runner")
	}

	result, err := plan.Run(ctx)
	if err != nil {
		ObserveExecution(plan.Service, plan.Operation, plan.Trigger, "error")
		return SecondaryExecution{
			Outcome: "error",
			Result:  result,
		}, err
	}

	outcome := secondaryOutcome(result)
	ObserveExecution(plan.Service, plan.Operation, plan.Trigger, outcome)
	return SecondaryExecution{
		Outcome: outcome,
		Result:  result,
	}, nil
}

func secondaryOutcome(result SecondaryResult) string {
	switch {
	case result.Successes == 0:
		return "error"
	case result.Items > 0:
		return "hit"
	default:
		return "miss"
	}
}

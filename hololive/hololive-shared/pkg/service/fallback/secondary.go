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

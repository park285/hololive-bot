package settings

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/workercontract"
)

func validateWorkerShapes(workers map[string]workercontract.WorkerProfile, shapes map[string]workerShape) []string {
	problems := make([]string, 0)
	for workerID, shape := range shapes {
		worker := workers[workerID]
		if worker.Executor.AttemptTimeout.Mode != shape.attemptTimeout {
			problems = append(problems, workerID+" attempt_timeout mode mismatch")
		}
		if worker.Queue.Capacity.Mode != shape.capacity {
			problems = append(problems, workerID+" capacity mode mismatch")
		}
		if worker.Queue.MaxAge.Mode != shape.maxAge {
			problems = append(problems, workerID+" max_age mode mismatch")
		}
	}
	return problems
}

func positiveValueProblems(values map[string]int64) []string {
	problems := make([]string, 0)
	for name, value := range values {
		if value < 1 || value > int64((30*24*time.Hour)/time.Millisecond) {
			problems = append(problems, name+" must be in 1..2592000000")
		}
	}
	return problems
}

func positiveIntProblems(values map[string]int) []string {
	problems := make([]string, 0)
	for name, value := range values {
		if value < 1 {
			problems = append(problems, name+" must be positive")
		}
	}
	return problems
}

func allPositiveInts(values ...int) bool {
	for _, value := range values {
		if value < 1 {
			return false
		}
	}
	return true
}

func joinWorkerProfileProblems(role string, problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return fmt.Errorf("validate Hololive %s worker profile: %s", role, strings.Join(problems, "; "))
}

func workerDuration(policy workercontract.DurationPolicy) time.Duration {
	if policy.Milliseconds == nil {
		return 0
	}
	return time.Duration(*policy.Milliseconds) * time.Millisecond
}

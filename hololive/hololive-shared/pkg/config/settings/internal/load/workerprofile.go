package load

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
)

// PositiveValueProblems: 밀리초 설정이 1..30일 범위를 벗어나면 문제로 보고한다.
func PositiveValueProblems(values map[string]int64) []string {
	problems := make([]string, 0)

	for name, value := range values {
		if value < 1 || value > int64((30*24*time.Hour)/time.Millisecond) {
			problems = append(problems, name+" must be in 1..2592000000")
		}
	}

	return problems
}

func PositiveIntProblems(values map[string]int) []string {
	problems := make([]string, 0)

	for name, value := range values {
		if value < 1 {
			problems = append(problems, name+" must be positive")
		}
	}

	return problems
}

func AllPositiveInts(values ...int) bool {
	for _, value := range values {
		if value < 1 {
			return false
		}
	}

	return true
}

func JoinWorkerProfileProblems(role string, problems []string) error {
	if len(problems) == 0 {
		return nil
	}

	slices.Sort(problems)

	return fmt.Errorf("validate Hololive %s worker profile: %s", role, strings.Join(problems, "; "))
}

func WorkerDuration(policy workercontract.DurationPolicy) time.Duration {
	if policy.Milliseconds == nil {
		return 0
	}

	return time.Duration(*policy.Milliseconds) * time.Millisecond
}

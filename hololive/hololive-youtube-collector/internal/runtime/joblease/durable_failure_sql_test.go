package joblease

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestOrdinaryDeferSQLHasClosedValuesWhitelist(t *testing.T) {
	t.Parallel()

	got := extractDurableFailureTuples(mustSQL("repository_lease_defer_0144_12.sql"))
	want := contract.DeferFailureTuples()

	if !failureTupleSetsEqual(got, want) {
		t.Fatalf("ordinary defer SQL tuples = %#v, want %#v", got, want)
	}
}

func failureTupleSetsEqual(got, want []contract.FailureTuple) bool {
	g := slices.Clone(got)
	w := slices.Clone(want)

	slices.SortFunc(g, compareTestFailureTuple)
	slices.SortFunc(w, compareTestFailureTuple)

	return slices.Equal(g, w)
}

func compareTestFailureTuple(a, b contract.FailureTuple) int {
	if a.Code != b.Code {
		if a.Code < b.Code {
			return -1
		}

		return 1
	}

	if a.Class == b.Class {
		return 0
	}

	if a.Class < b.Class {
		return -1
	}

	return 1
}

var durableFailureTuplePattern = regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,\s*'([A-Z_]+)'\s*\)`)

func extractDurableFailureTuples(sql string) []contract.FailureTuple {
	upper := strings.ToUpper(sql)
	valuesAt := strings.Index(upper, "VALUES")

	if valuesAt < 0 {
		return nil
	}

	matches := durableFailureTuplePattern.FindAllStringSubmatch(sql[valuesAt:], -1)
	if len(matches) == 0 {
		return nil
	}

	tuples := make([]contract.FailureTuple, 0, len(matches))
	for _, match := range matches {
		tuples = append(tuples, contract.FailureTuple{
			Code:  contract.CollectionErrorCode(match[1]),
			Class: contract.FailureClass(match[2]),
		})
	}

	return tuples
}

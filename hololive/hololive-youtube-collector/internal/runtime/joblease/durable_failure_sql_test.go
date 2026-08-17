package joblease

import (
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

func TestReleaseSQLMatchesReleasableCodesAndPreservesFailureHistory(t *testing.T) {
	t.Parallel()
	sql := mustSQL("repository_lease_release_0144_10.sql")
	got := extractReleaseCodes(sql)
	want := contract.ReleasableCollectionErrorCodes()
	g := slices.Clone(got)
	w := slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Fatalf("release SQL codes = %#v, want %#v", g, w)
	}
	if strings.Contains(sql, "last_failure_code") || strings.Contains(sql, "last_failure_class") ||
		strings.Contains(sql, "last_failure_detail") || strings.Contains(sql, "last_failure_at") {
		t.Fatal("release SQL must not write last_failure_*")
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

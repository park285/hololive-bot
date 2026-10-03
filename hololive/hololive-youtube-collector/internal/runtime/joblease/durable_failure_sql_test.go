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

var durableFailureTuplePattern = regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*,\s*'([A-Z_]+)'\s*\)`)

var releaseCodePattern = regexp.MustCompile(`\(\s*'([a-z0-9_]+)'\s*\)`)

func extractReleaseCodes(sql string) []contract.CollectionErrorCode {
	upper := strings.ToUpper(sql)
	valuesAt := strings.Index(upper, "VALUES")

	if valuesAt < 0 {
		return nil
	}

	matches := releaseCodePattern.FindAllStringSubmatch(sql[valuesAt:], -1)
	if len(matches) == 0 {
		return nil
	}

	codes := make([]contract.CollectionErrorCode, 0, len(matches))
	seen := make(map[contract.CollectionErrorCode]struct{}, len(matches))

	for _, match := range matches {
		code := contract.CollectionErrorCode(match[1])
		if _, ok := seen[code]; ok {
			continue
		}

		seen[code] = struct{}{}
		codes = append(codes, code)
	}

	return codes
}

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

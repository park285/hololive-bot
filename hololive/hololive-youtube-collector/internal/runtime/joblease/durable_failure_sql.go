package joblease

import (
	"regexp"
	"strings"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

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

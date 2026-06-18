package observationquery

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errRequired = errors.New("observation-runtime and observation-cutover are required")
var errMustBeProvidedTogether = errors.New("observation-runtime and observation-cutover must be provided together")

type Query struct {
	Runtime   string
	CutoverAt time.Time
}

func ParseRequired(runtime, cutover string) (Query, error) {
	query, ok, err := ParseOptional(runtime, cutover)
	if err != nil {
		return Query{}, err
	}
	if !ok {
		return Query{}, errRequired
	}
	return query, nil
}

func ParseOptional(runtime, cutover string) (Query, bool, error) {
	trimmedRuntime := strings.TrimSpace(runtime)
	trimmedCutover := strings.TrimSpace(cutover)

	switch {
	case trimmedRuntime == "" && trimmedCutover == "":
		return Query{}, false, nil
	case trimmedRuntime == "" || trimmedCutover == "":
		return Query{}, false, errMustBeProvidedTogether
	}

	parsedCutoverAt, err := time.Parse(time.RFC3339, trimmedCutover)
	if err != nil {
		return Query{}, false, fmt.Errorf("invalid observation-cutover %q: %w", cutover, err)
	}

	return Query{
		Runtime:   trimmedRuntime,
		CutoverAt: parsedCutoverAt.UTC(),
	}, true, nil
}

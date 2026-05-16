package scraping

import (
	"errors"
	"fmt"
)

var ErrParserDrift = errors.New("youtube parser drift")

type ParserDriftError struct {
	Operation string
	Stage     string
	Cause     error
}

func (e *ParserDriftError) Error() string {
	if e == nil {
		return "youtube parser drift"
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s parser drift at %s", e.Operation, e.Stage)
	}
	return fmt.Sprintf("%s parser drift at %s: %v", e.Operation, e.Stage, e.Cause)
}

func (e *ParserDriftError) Unwrap() error {
	if e == nil {
		return nil
	}
	return errors.Join(ErrParserDrift, e.Cause)
}

func NewParserDriftError(operation, stage string, cause error) error {
	return &ParserDriftError{Operation: operation, Stage: stage, Cause: cause}
}

func IsParserDriftError(err error) bool {
	return errors.Is(err, ErrParserDrift)
}

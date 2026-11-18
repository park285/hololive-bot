package matcher

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type AmbiguousMatchError struct {
	Query      string
	Candidates []*domain.Member
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("ambiguous match for %q: %d candidates found", e.Query, len(e.Candidates))
}

func NewAmbiguousMatchError(query string, candidates []*domain.Member) *AmbiguousMatchError {
	return &AmbiguousMatchError{
		Query:      query,
		Candidates: candidates,
	}
}

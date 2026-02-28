package matcher

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// AmbiguousMatchError: 동명이인으로 인해 정확한 매칭을 할 수 없을 때 반환되는 에러
type AmbiguousMatchError struct {
	Query      string
	Candidates []*domain.Member
}

func (e *AmbiguousMatchError) Error() string {
	return fmt.Sprintf("ambiguous match for %q: %d candidates found", e.Query, len(e.Candidates))
}

// NewAmbiguousMatchError: 새로운 AmbiguousMatchError를 생성합니다.
func NewAmbiguousMatchError(query string, candidates []*domain.Member) *AmbiguousMatchError {
	return &AmbiguousMatchError{
		Query:      query,
		Candidates: candidates,
	}
}

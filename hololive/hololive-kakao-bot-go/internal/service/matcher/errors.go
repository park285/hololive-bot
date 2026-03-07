// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

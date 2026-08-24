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

package cache

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

type CacheError struct {
	Operation string // get, set, delete 등
	Key       string // cache key (선택)
	Err       error  // 원인 에러
}

// Key는 원문을 유지한다. 이 에러는 상위 plane에서 slog.Any("error", …)로 그대로 실려나가므로
// 문자열 표현만 비식별화한다.
func (e *CacheError) Error() string {
	loggableKey := privacylog.RedactCacheKey(e.Key)

	if e.Err == nil {
		if e.Key == "" {
			return fmt.Sprintf("cache: %s", e.Operation)
		}

		return fmt.Sprintf("cache: %s: key=%s", e.Operation, loggableKey)
	}

	if e.Key == "" {
		return fmt.Sprintf("cache: %s: %v", e.Operation, e.Err)
	}

	return fmt.Sprintf("cache: %s: key=%s: %v", e.Operation, loggableKey, e.Err)
}

func (e *CacheError) Unwrap() error { return e.Err }

// NewCacheError는 cache 에러를 생성합니다.
func NewCacheError(operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Err:       cause,
	}
}

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

import "fmt"

type CacheError struct {
	Operation string // get, set, delete 등
	Key       string // cache key (선택)
	Err       error  // 원인 에러
}

func (e *CacheError) Error() string {
	if e.Err == nil {
		if e.Key == "" {
			return fmt.Sprintf("cache: %s", e.Operation)
		}
		return fmt.Sprintf("cache: %s: key=%s", e.Operation, e.Key)
	}

	if e.Key == "" {
		return fmt.Sprintf("cache: %s: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("cache: %s: key=%s: %v", e.Operation, e.Key, e.Err)
}

func (e *CacheError) Unwrap() error { return e.Err }

// NewCacheError는 cache 에러를 생성합니다.
//
// message 파라미터는 레거시 호환을 위해 유지합니다(현재 Error() 문자열에는 포함하지 않음).
func NewCacheError(_, operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Err:       cause,
	}
}

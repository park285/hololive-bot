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

package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// FetchProfilesRuntime: 프로필 수집 작업을 실행하기 위한 런타임 환경
type FetchProfilesRuntime struct {
	Logger     *slog.Logger
	HTTPClient *http.Client

	cleanup func()
}

// Close: 런타임 리소스를 정리합니다.
func (r *FetchProfilesRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildFetchProfilesRuntime: 프로필 수집 런타임 환경을 구성하고 초기화합니다.
func BuildFetchProfilesRuntime(ctx context.Context) (*FetchProfilesRuntime, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, cleanup, err := InitializeFetchProfilesRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize fetch profiles runtime: %w", err)
	}
	runtime.cleanup = cleanup

	return runtime, nil
}

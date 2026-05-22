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
	"context"
	"testing"
)

func TestCompareAndDelete(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Service, context.Context)
		key        string
		expected   string
		wantResult bool
	}{
		{
			name: "value matches - should delete and return true",
			setup: func(service *Service, ctx context.Context) {
				// 직접 SET으로 raw 값 저장 (json 래핑 없이)
				cmd := service.client.B().Set().Key("cas:match").Value("secret-token").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas:match",
			expected:   "secret-token",
			wantResult: true,
		},
		{
			name: "value does not match - should not delete and return false",
			setup: func(service *Service, ctx context.Context) {
				cmd := service.client.B().Set().Key("cas:mismatch").Value("actual-value").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas:mismatch",
			expected:   "wrong-value",
			wantResult: false,
		},
		{
			name:       "key does not exist - should return false",
			setup:      nil,
			key:        "cas:nonexistent",
			expected:   "any-value",
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, _ := newTestCacheService(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(service, ctx)
			}

			got, err := service.CompareAndDelete(ctx, tt.key, tt.expected)
			if err != nil {
				t.Fatalf("CompareAndDelete() error = %v", err)
			}
			if got != tt.wantResult {
				t.Errorf("CompareAndDelete() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

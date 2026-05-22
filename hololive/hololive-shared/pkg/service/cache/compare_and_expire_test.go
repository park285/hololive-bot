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
	"time"
)

func TestCompareAndExpire(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Service, context.Context)
		key        string
		expected   string
		wantResult bool
		wantExists bool
	}{
		{
			name: "value matches - should expire key after ttl",
			setup: func(service *Service, ctx context.Context) {
				cmd := service.client.B().Set().Key("cas-expire:match").Value("owner-a").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas-expire:match",
			expected:   "owner-a",
			wantResult: true,
			wantExists: false,
		},
		{
			name: "value mismatch - should keep key",
			setup: func(service *Service, ctx context.Context) {
				cmd := service.client.B().Set().Key("cas-expire:mismatch").Value("owner-b").Build()
				service.client.Do(ctx, cmd)
			},
			key:        "cas-expire:mismatch",
			expected:   "owner-other",
			wantResult: false,
			wantExists: true,
		},
		{
			name:       "missing key - should return false",
			setup:      nil,
			key:        "cas-expire:missing",
			expected:   "owner-any",
			wantResult: false,
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, mini := newTestCacheService(t)
			ctx := context.Background()

			if tt.setup != nil {
				tt.setup(service, ctx)
			}

			got, err := service.CompareAndExpire(ctx, tt.key, tt.expected, time.Second)
			if err != nil {
				t.Fatalf("CompareAndExpire() error = %v", err)
			}
			if got != tt.wantResult {
				t.Fatalf("CompareAndExpire() = %v, want %v", got, tt.wantResult)
			}

			mini.FastForward(2 * time.Second)
			exists, err := service.Exists(ctx, tt.key)
			if err != nil {
				t.Fatalf("Exists() error = %v", err)
			}
			if exists != tt.wantExists {
				t.Fatalf("Exists() = %v, want %v", exists, tt.wantExists)
			}
		})
	}
}

func TestCompareAndExpireInvalidTTL(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx := context.Background()

	if _, err := service.CompareAndExpire(ctx, "k", "v", 0); err == nil {
		t.Fatalf("expected error for zero ttl")
	}
}

func TestCompareAndExpireCeilSeconds(t *testing.T) {
	service, mini := newTestCacheService(t)
	ctx := context.Background()

	if err := service.GetClient().Do(ctx, service.B().Set().Key("cas-expire:ceil").Value("owner").Build()).Error(); err != nil {
		t.Fatalf("set key: %v", err)
	}

	ok, err := service.CompareAndExpire(ctx, "cas-expire:ceil", "owner", 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("CompareAndExpire() error = %v", err)
	}
	if !ok {
		t.Fatalf("CompareAndExpire() = false, want true")
	}

	mini.FastForward(1400 * time.Millisecond)
	exists, err := service.Exists(ctx, "cas-expire:ceil")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Fatalf("key expired too early; ttl should be ceil-rounded")
	}

	mini.FastForward(700 * time.Millisecond)
	exists, err = service.Exists(ctx, "cas-expire:ceil")
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists {
		t.Fatalf("key should be expired after rounded ttl elapsed")
	}
}

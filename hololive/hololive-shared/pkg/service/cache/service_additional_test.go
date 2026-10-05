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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheServiceScanKeyPagesAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "matching pattern returns expected keys",
			pattern: "scan:user:*",
			want:    []string{"scan:user:1", "scan:user:2"},
		},
		{
			name:    "non matching pattern returns empty",
			pattern: "scan:missing:*",
			want:    []string{},
		},
		{
			name:    "wildcard returns all keys",
			pattern: "*",
			want:    []string{"scan:user:1", "scan:user:2", "scan:stream:1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()
			seedKeys := []string{"scan:user:1", "scan:user:2", "scan:stream:1"}

			for _, key := range seedKeys {
				require.NoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
			}

			var got []string

			err := service.ScanKeyPages(ctx, tt.pattern, 2, func(keys []string) error {
				require.NotEmpty(t, keys, "empty SCAN pages must not reach visit")

				got = append(got, keys...)

				return nil
			})

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestCacheServiceScanKeyPagesStopsOnVisitError(t *testing.T) {
	t.Parallel()

	service, _ := newTestCacheService(t)
	ctx := t.Context()

	for i := range 10 {
		require.NoError(t, service.Set(ctx, fmt.Sprintf("scan:stop:%d", i), testPayload{Name: "stop"}, 0))
	}

	visitErr := errors.New("stop scan")
	visits := 0

	err := service.ScanKeyPages(ctx, "scan:stop:*", 1, func([]string) error {
		visits++

		return visitErr
	})

	require.ErrorIs(t, err, visitErr)
	assert.Equal(t, 1, visits, "a visit error must stop the scan before the next page")
}

func TestCacheServiceDelManyAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		seedKeys    []string
		deleteKeys  []string
		wantDeleted int64
		wantRemain  []string
	}{
		{
			name:        "deletes multiple keys successfully",
			seedKeys:    []string{"del:multi:a", "del:multi:b", "del:multi:c"},
			deleteKeys:  []string{"del:multi:a", "del:multi:b", "del:multi:c"},
			wantDeleted: 3,
		},
		{
			name:        "empty key list is no op",
			seedKeys:    []string{"del:empty:kept"},
			deleteKeys:  []string{},
			wantDeleted: 0,
			wantRemain:  []string{"del:empty:kept"},
		},
		{
			name:        "chunked delete removes all requested keys",
			seedKeys:    numberedKeys("del:chunk:", 1001),
			deleteKeys:  numberedKeys("del:chunk:", 1001),
			wantDeleted: 1001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()

			for _, key := range tt.seedKeys {
				require.NoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
			}

			deleted, err := service.DelMany(ctx, tt.deleteKeys)

			require.NoError(t, err)
			assert.Equal(t, tt.wantDeleted, deleted)

			for _, key := range tt.deleteKeys {
				exists, existsErr := service.Exists(ctx, key)
				require.NoError(t, existsErr)
				assert.False(t, exists, "expected %s to be deleted", key)
			}

			for _, key := range tt.wantRemain {
				exists, existsErr := service.Exists(ctx, key)
				require.NoError(t, existsErr)
				assert.True(t, exists, "expected %s to remain", key)
			}
		})
	}
}

func TestCacheServiceExistsAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, service *Service, ctx context.Context)
		key   string
		want  bool
	}{
		{
			name: "returns true for existing key",
			setup: func(t *testing.T, service *Service, ctx context.Context) {
				t.Helper()
				require.NoError(t, service.Set(ctx, "exists:present", testPayload{Name: testPayloadValue}, 0))
			},
			key:  "exists:present",
			want: true,
		},
		{
			name: "returns false for non existing key",
			key:  "exists:missing",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()

			if tt.setup != nil {
				tt.setup(t, service, ctx)
			}

			got, err := service.Exists(ctx, tt.key)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCacheServiceWaitUntilReadyTickAdditional(t *testing.T) {
	t.Parallel()

	t.Run("returns ready when connected", func(t *testing.T) {
		t.Parallel()

		service, _ := newTestCacheService(t)
		ctx := t.Context()
		ticks := make(chan time.Time, 1)

		ticks <- time.Now()

		ready, err := service.waitUntilReadyTick(ctx, ticks)

		require.NoError(t, err)
		assert.True(t, ready)
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		t.Parallel()

		service, _ := newTestCacheService(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		ready, err := service.waitUntilReadyTick(ctx, make(chan time.Time))

		require.Error(t, err)
		assert.False(t, ready)
	})
}

func numberedKeys(prefix string, count int) []string {
	keys := make([]string, 0, count)
	for i := range count {
		keys = append(keys, fmt.Sprintf("%s%d", prefix, i))
	}

	return keys
}

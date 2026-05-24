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
	"fmt"
	"testing"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheServiceScanKeysAdditional(t *testing.T) {
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
			ctx := context.Background()
			seedKeys := []string{"scan:user:1", "scan:user:2", "scan:stream:1"}
			for _, key := range seedKeys {
				require.NoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
			}

			got, err := service.ScanKeys(ctx, tt.pattern, 2)

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
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
			ctx := context.Background()
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

func TestCacheServiceSetNXMultiAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		existing      map[string]string
		entries       []SetNXEntry
		wantResults   []SetNXResult
		wantRawValues map[string]string
	}{
		{
			name: "sets all keys when none exist",
			entries: []SetNXEntry{
				{Key: "setnx:all:a", Value: "A"},
				{Key: "setnx:all:b", Value: "B", TTL: time.Minute},
			},
			wantResults: []SetNXResult{
				{Key: "setnx:all:a", Acquired: true},
				{Key: "setnx:all:b", Acquired: true},
			},
			wantRawValues: map[string]string{
				"setnx:all:a": "A",
				"setnx:all:b": "B",
			},
		},
		{
			name: "sets no keys when all already exist",
			existing: map[string]string{
				"setnx:existing:a": "old-a",
				"setnx:existing:b": "old-b",
			},
			entries: []SetNXEntry{
				{Key: "setnx:existing:a", Value: "new-a"},
				{Key: "setnx:existing:b", Value: "new-b"},
			},
			wantResults: []SetNXResult{
				{Key: "setnx:existing:a", Acquired: false},
				{Key: "setnx:existing:b", Acquired: false},
			},
			wantRawValues: map[string]string{
				"setnx:existing:a": "old-a",
				"setnx:existing:b": "old-b",
			},
		},
		{
			name: "sets only new keys in mixed batch",
			existing: map[string]string{
				"setnx:mixed:existing": "old",
			},
			entries: []SetNXEntry{
				{Key: "setnx:mixed:existing", Value: "new"},
				{Key: "setnx:mixed:new", Value: "created"},
			},
			wantResults: []SetNXResult{
				{Key: "setnx:mixed:existing", Acquired: false},
				{Key: "setnx:mixed:new", Acquired: true},
			},
			wantRawValues: map[string]string{
				"setnx:mixed:existing": "old",
				"setnx:mixed:new":      "created",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := context.Background()
			for key, value := range tt.existing {
				acquired, err := service.SetNX(ctx, key, value, 0)
				require.NoError(t, err)
				require.True(t, acquired)
			}

			got, err := service.SetNXMulti(ctx, tt.entries)

			require.NoError(t, err)
			require.Len(t, got, len(tt.wantResults))
			for i, want := range tt.wantResults {
				assert.Equal(t, want.Key, got[i].Key)
				assert.Equal(t, want.Acquired, got[i].Acquired)
				assert.NoError(t, got[i].Err)
			}
			for key, want := range tt.wantRawValues {
				gotValue, hit, getErr := service.GetString(ctx, key)
				require.NoError(t, getErr)
				require.True(t, hit)
				assert.Equal(t, want, gotValue)
			}
		})
	}
}

func TestCacheServiceMSetMGetAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pairs     map[string]any
		keys      []string
		wantItems map[string]testPayload
	}{
		{
			name: "mset multiple keys then mget retrieves them all",
			pairs: map[string]any{
				"mget:all:a": testPayload{Name: "A"},
				"mget:all:b": testPayload{Name: "B"},
			},
			keys: []string{"mget:all:a", "mget:all:b"},
			wantItems: map[string]testPayload{
				"mget:all:a": {Name: "A"},
				"mget:all:b": {Name: "B"},
			},
		},
		{
			name: "mget returns existing keys from mixed request",
			pairs: map[string]any{
				"mget:mixed:existing": testPayload{Name: "existing"},
			},
			keys: []string{"mget:mixed:existing", "mget:mixed:missing"},
			wantItems: map[string]testPayload{
				"mget:mixed:existing": {Name: "existing"},
			},
		},
		{
			name:      "mget empty key list returns empty map",
			pairs:     map[string]any{},
			keys:      []string{},
			wantItems: map[string]testPayload{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := context.Background()
			require.NoError(t, service.MSet(ctx, tt.pairs, 0))

			got, err := service.MGet(ctx, tt.keys)

			require.NoError(t, err)
			assert.Equal(t, tt.wantItems, decodePayloadMap(t, got))
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
				require.NoError(t, service.Set(ctx, "exists:present", testPayload{Name: "value"}, 0))
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
			ctx := context.Background()
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
		ctx := context.Background()
		ticks := make(chan time.Time, 1)
		ticks <- time.Now()

		ready, err := service.waitUntilReadyTick(ctx, ticks)

		require.NoError(t, err)
		assert.True(t, ready)
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		t.Parallel()

		service, _ := newTestCacheService(t)
		ctx, cancel := context.WithCancel(context.Background())
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

func decodePayloadMap(t *testing.T, values map[string]string) map[string]testPayload {
	t.Helper()

	decoded := make(map[string]testPayload, len(values))
	for key, value := range values {
		var payload testPayload
		require.NoError(t, json.Unmarshal([]byte(value), &payload))
		decoded[key] = payload
	}
	return decoded
}

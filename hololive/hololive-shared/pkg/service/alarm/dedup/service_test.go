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

package dedup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

var newTestLogger = sharedlogging.NewLogger

type mockDedupCacheState struct {
	mu      sync.Mutex
	now     func() time.Time
	setNX   map[string]time.Time
	hashes  map[string]map[string]string
	strings map[string]string
}

func newMockDedupCache(t *testing.T) (*cachemocks.Client, *mockDedupCacheState) {
	t.Helper()

	state := &mockDedupCacheState{
		now:     time.Now,
		setNX:   make(map[string]time.Time),
		hashes:  make(map[string]map[string]string),
		strings: make(map[string]string),
	}

	client := &cachemocks.Client{}
	client.SetNXFunc = func(_ context.Context, key, _ string, ttl time.Duration) (bool, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		now := state.now()
		if expiresAt, exists := state.setNX[key]; exists && now.Before(expiresAt) {
			return false, nil
		}

		if ttl <= 0 {
			state.setNX[key] = now
		} else {
			state.setNX[key] = now.Add(ttl)
		}
		return true, nil
	}
	client.DelManyFunc = func(_ context.Context, keys []string) (int64, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		var removed int64
		for _, key := range keys {
			existed := false
			if _, ok := state.setNX[key]; ok {
				delete(state.setNX, key)
				existed = true
			}
			if _, ok := state.hashes[key]; ok {
				delete(state.hashes, key)
				existed = true
			}
			if _, ok := state.strings[key]; ok {
				delete(state.strings, key)
				existed = true
			}
			if existed {
				removed++
			}
		}
		return removed, nil
	}
	client.DelFunc = func(_ context.Context, key string) error {
		state.mu.Lock()
		defer state.mu.Unlock()
		delete(state.setNX, key)
		delete(state.hashes, key)
		delete(state.strings, key)
		return nil
	}
	client.HGetFunc = func(_ context.Context, key, field string) (string, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		if _, ok := state.strings[key]; ok {
			return "", errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		if fields, ok := state.hashes[key]; ok {
			return fields[field], nil
		}
		return "", nil
	}
	client.HSetFunc = func(_ context.Context, key, field, value string) error {
		state.mu.Lock()
		defer state.mu.Unlock()

		if _, ok := state.strings[key]; ok {
			return errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		fields, ok := state.hashes[key]
		if !ok {
			fields = make(map[string]string)
			state.hashes[key] = fields
		}
		fields[field] = value
		return nil
	}
	client.HMSetFunc = func(_ context.Context, key string, fields map[string]any) error {
		state.mu.Lock()
		defer state.mu.Unlock()

		if _, ok := state.strings[key]; ok {
			return errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		hash, ok := state.hashes[key]
		if !ok {
			hash = make(map[string]string)
			state.hashes[key] = hash
		}
		for field, value := range fields {
			hash[field] = fmt.Sprint(value)
		}
		return nil
	}
	client.HGetAllFunc = func(_ context.Context, key string) (map[string]string, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		if _, ok := state.strings[key]; ok {
			return nil, errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		fields, ok := state.hashes[key]
		if !ok {
			return map[string]string{}, nil
		}
		copied := make(map[string]string, len(fields))
		for k, v := range fields {
			copied[k] = v
		}
		return copied, nil
	}
	client.ExpireFunc = func(_ context.Context, _ string, _ time.Duration) error {
		return nil
	}
	client.SetFunc = func(_ context.Context, key string, value any, _ time.Duration) error {
		state.mu.Lock()
		defer state.mu.Unlock()

		switch v := value.(type) {
		case string:
			state.strings[key] = v
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("mock set: marshal value: %w", err)
			}
			state.strings[key] = string(encoded)
		}
		return nil
	}
	client.GetFunc = func(_ context.Context, key string, dest any) error {
		state.mu.Lock()
		raw := state.strings[key]
		state.mu.Unlock()

		if dest == nil {
			return nil
		}
		if raw == "" {
			return nil
		}

		if ptr, ok := dest.(*string); ok {
			*ptr = raw
			return nil
		}

		if err := json.Unmarshal([]byte(raw), dest); err != nil {
			return fmt.Errorf("mock get: unmarshal value: %w", err)
		}
		return nil
	}

	return client, state
}

func (s *mockDedupCacheState) setRawString(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.strings[key] = value
}

func fallbackKeyCount(fb *LocalFallback) int {
	count := 0
	fb.keys.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// --- LocalFallback 테스트 ---

func TestLocalFallback_TryClaimReleaseAndExpiry(t *testing.T) {
	current := time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)
	fb := NewLocalFallback(newTestLogger())
	fb.now = func() time.Time { return current }

	assert.True(t, fb.tryClaim("claim:key", time.Minute))
	assert.False(t, fb.tryClaim("claim:key", time.Minute))

	fb.ReleaseClaims([]string{"claim:key"})
	assert.True(t, fb.tryClaim("claim:key", time.Minute))
	assert.False(t, fb.tryClaim("claim:key", time.Minute))

	current = current.Add(2 * time.Minute)
	assert.True(t, fb.tryClaim("claim:key", time.Minute))
}

func TestLocalFallback_CleanupExpiredEntriesOnCapacity(t *testing.T) {
	current := time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)
	fb := NewLocalFallback(newTestLogger())
	fb.now = func() time.Time { return current }

	for i := 0; i < constants.LocalFallbackCleanupMaxKeys; i++ {
		require.True(t, fb.tryClaim(fmt.Sprintf("expired:%d", i), time.Second))
	}
	require.Equal(t, constants.LocalFallbackCleanupMaxKeys, fallbackKeyCount(fb))

	current = current.Add(2 * time.Second)
	require.True(t, fb.tryClaim("fresh:key", time.Minute))
	assert.Equal(t, 1, fallbackKeyCount(fb))
}

func TestNormalizeFallbackTTL(t *testing.T) {
	tests := []struct {
		name     string
		ttl      time.Duration
		expected time.Duration
	}{
		{"zero -> default", 0, constants.LocalFallbackDedupTTL},
		{"over max -> default", time.Hour, constants.LocalFallbackDedupTTL},
		{"within range", 5 * time.Minute, 5 * time.Minute},
		{"exact max", constants.LocalFallbackDedupTTL, constants.LocalFallbackDedupTTL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, normalizeFallbackTTL(tc.ttl))
		})
	}
}

// --- DedupService 테스트 ---

func TestService_TryClaimNotification_ClaimKeyCategoryAndSchedulePolicy(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 9, 15, 5, 0, time.UTC)

	keyTarget, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildNotifyClaimKey("room1", "vid1", start, "target"), keyTarget)

	keyTargetAgain, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 3)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, keyTarget, keyTargetAgain)

	keySameMinute, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start.Add(30*time.Second), 5)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, keyTarget, keySameMinute)

	keyNonTarget, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 10)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildNotifyClaimKey("room1", "vid1", start, "10"), keyNonTarget)
	assert.NotEqual(t, keyTarget, keyNonTarget)

	keyDifferentSchedule, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start.Add(time.Minute), 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.NotEqual(t, keyTarget, keyDifferentSchedule)
}

func TestService_TryClaimLogicalEventAndScheduleTransition(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "stream1",
		Title:          "테스트 방송",
		StartScheduled: &start,
	}

	logicalKey, acquired, err := svc.TryClaimLogicalEvent(t.Context(), "room1", "UC_TEST", stream, 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildLogicalEventClaimKey("room1", "UC_TEST", stream.ID, stream.Title, start, "target"), logicalKey)

	logicalKeyAgain, acquired, err := svc.TryClaimLogicalEvent(t.Context(), "room1", "UC_TEST", stream, 3)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, logicalKey, logicalKeyAgain)

	transitionKey, acquired, err := svc.TryClaimScheduleTransition(t.Context(), stream.ID, start, start.Add(30*time.Minute))
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildScheduleTransitionKey(stream.ID, start, start.Add(30*time.Minute)), transitionKey)

	transitionKeyAgain, acquired, err := svc.TryClaimScheduleTransition(t.Context(), stream.ID, start, start.Add(30*time.Minute))
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, transitionKey, transitionKeyAgain)
}

func TestService_MarkAsNotified_TargetMinutePolicyAndScheduleReset(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "vid-notified"
	start := time.Date(2026, 3, 4, 10, 0, 12, 0, time.UTC)

	require.NoError(t, svc.MarkAsNotified(t.Context(), streamID, start, 5))

	already, err := svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 3)
	require.NoError(t, err)
	assert.True(t, already, "target 분은 같은 스케줄에서 1회만 허용")

	already, err = svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 10)
	require.NoError(t, err)
	assert.False(t, already, "non-target 분은 개별 분 기준")

	require.NoError(t, svc.MarkAsNotified(t.Context(), streamID, start, 10))
	already, err = svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 10)
	require.NoError(t, err)
	assert.True(t, already)

	anyNotified, err := svc.IsAlreadyNotified(t.Context(), streamID)
	require.NoError(t, err)
	assert.True(t, anyNotified)

	changed := start.Add(2 * time.Hour)
	already, err = svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, changed, 5)
	require.NoError(t, err)
	assert.False(t, already, "스케줄이 바뀌면 이전 이력은 무시")

	require.NoError(t, svc.MarkAsNotified(t.Context(), streamID, changed, 5))

	already, err = svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 5)
	require.NoError(t, err)
	assert.False(t, already, "새 스케줄 기록 후 기존 스케줄은 차단되지 않아야 함")

	already, err = svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, changed, 5)
	require.NoError(t, err)
	assert.True(t, already)
}

func TestService_LegacyStringNotifiedData_MigratesToHash(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "vid-legacy"
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	key := keys.NotifiedKey(streamID)

	legacyJSON, err := json.Marshal(NotifiedData{
		StartScheduled: keys.FormatScheduled(start),
		SentAt:         map[int]bool{5: true},
	})
	require.NoError(t, err)
	state.setRawString(key, string(legacyJSON))

	already, err := svc.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 5)
	require.NoError(t, err)
	assert.True(t, already)

	state.mu.Lock()
	_, hasLegacyString := state.strings[key]
	hashFields := maps.Clone(state.hashes[key])
	state.mu.Unlock()

	assert.False(t, hasLegacyString)
	require.NotNil(t, hashFields)
	assert.Equal(t, keys.FormatScheduled(start), hashFields["start_scheduled"])
	assert.Equal(t, "1", hashFields["5"])

	require.NoError(t, svc.MarkAsNotified(t.Context(), streamID, start, 3))

	state.mu.Lock()
	hashFields = maps.Clone(state.hashes[key])
	state.mu.Unlock()

	assert.Equal(t, "1", hashFields["3"])
	assert.Equal(t, "1", hashFields["5"])
}

func TestService_UpcomingEventRecentlyWindow(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "vid-upcoming",
		Title:          "Upcoming",
		StartScheduled: &start,
	}

	require.NoError(t, svc.MarkUpcomingEventNotified(t.Context(), "room1", "UC_TEST", stream))

	recent, err := svc.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, recent)

	key := keys.BuildUpcomingEventKey("room1", "UC_TEST", stream.ID, stream.Title, start)
	stale := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	staleJSON, err := json.Marshal(stale)
	require.NoError(t, err)
	state.setRawString(key, string(staleJSON))

	recent, err = svc.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, recent)
}

func TestService_WasUpcomingEventNotifiedRecently_InvalidPayloadReturnsError(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "vid-upcoming",
		Title:          "Upcoming",
		StartScheduled: &start,
	}
	key := keys.BuildUpcomingEventKey("room1", "UC_TEST", stream.ID, stream.Title, start)
	state.setRawString(key, "{invalid-json")

	_, err := svc.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
	require.Error(t, err)
	assert.ErrorContains(t, err, "was upcoming event notified recently: get cache data")
}

func TestService_TryClaimNotification_FallbackClaimOnSetNXFailure(t *testing.T) {
	cacheMock := &cachemocks.Client{
		SetNXFunc: func(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
			return false, errors.New("valkey outage")
		},
		DelManyFunc: func(_ context.Context, keys []string) (int64, error) {
			return int64(len(keys)), nil
		},
	}
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

	key, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.True(t, acquired)

	_, acquired, err = svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.False(t, acquired, "SETNX 실패 시에도 로컬 폴백으로 중복 차단돼야 함")

	require.NoError(t, svc.ReleaseClaims(t.Context(), []string{key}))

	_, acquired, err = svc.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.True(t, acquired, "release 후에는 재 claim 가능해야 함")
}

func TestService_ReleaseClaims_WrapsDelManyError(t *testing.T) {
	expectedErr := errors.New("forced delmany failure")
	cacheMock := &cachemocks.Client{
		DelManyFunc: func(_ context.Context, _ []string) (int64, error) {
			return 0, expectedErr
		},
	}
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	err := svc.ReleaseClaims(t.Context(), []string{"claim:key"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "release claims: del many keys")
	assert.ErrorIs(t, err, expectedErr)
}

func TestService_TryClaimNotification_ZeroTime(t *testing.T) {
	svc := NewService(nil, []int{5, 3, 1}, newTestLogger())
	key, acquired, err := svc.TryClaimNotification(t.Context(), "room1", "vid1", time.Time{}, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_TryClaimLogicalEvent_NilSchedule(t *testing.T) {
	svc := NewService(nil, []int{5, 3, 1}, newTestLogger())
	stream := &domain.Stream{ID: "vid1", Title: "test"}
	key, acquired, err := svc.TryClaimLogicalEvent(t.Context(), "room1", "UC_A", stream, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_TryClaimLogicalEvent_NilStream(t *testing.T) {
	svc := NewService(nil, []int{5, 3, 1}, newTestLogger())
	key, acquired, err := svc.TryClaimLogicalEvent(t.Context(), "room1", "UC_A", nil, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_ReadNotifiedData_LegacyJSONMigrated(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	svc := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	key := keys.NotifiedKey("legacy-stream")
	legacy := NotifiedData{
		StartScheduled: "2026-03-04T10:00:00Z",
		SentAt: map[int]bool{
			5: true,
		},
	}
	legacyJSON, err := json.Marshal(legacy)
	require.NoError(t, err)
	state.setRawString(key, string(legacyJSON))

	notified, err := svc.IsAlreadyNotifiedForSchedule(
		t.Context(),
		"legacy-stream",
		time.Date(2026, 3, 4, 10, 0, 1, 0, time.UTC),
		5,
	)
	require.NoError(t, err)
	assert.True(t, notified)

	state.mu.Lock()
	_, hasLegacyString := state.strings[key]
	hashFields := maps.Clone(state.hashes[key])
	state.mu.Unlock()

	assert.False(t, hasLegacyString)
	require.NotNil(t, hashFields)
	assert.Equal(t, "2026-03-04T10:00:00Z", hashFields["start_scheduled"])
	assert.Equal(t, "1", hashFields["5"])
}

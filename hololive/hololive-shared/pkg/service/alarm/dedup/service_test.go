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
	"errors"
	"fmt"
	"maps"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	json "github.com/park285/shared-go/pkg/json"
	sharedlogging "github.com/park285/shared-go/pkg/logging"
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

	client := cachemocks.NewStrictClient()
	configureMockDedupClaimCache(client, state)
	configureMockDedupKeyDeletion(client, state)
	configureMockDedupHashCache(client, state)
	configureMockDedupStringCache(client, state)

	return client, state
}

func configureMockDedupClaimCache(client *cachemocks.Client, state *mockDedupCacheState) {
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
}

func configureMockDedupKeyDeletion(client *cachemocks.Client, state *mockDedupCacheState) {
	client.DelManyFunc = func(_ context.Context, keys []string) (int64, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		var removed int64
		for _, key := range keys {
			if deleteMockDedupKey(state, key) {
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
	client.ScanKeysFunc = func(_ context.Context, pattern string, _ int64) ([]string, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		matches := make([]string, 0)
		for key := range state.strings {
			ok, err := path.Match(pattern, key)
			if err != nil {
				return nil, err
			}
			if ok {
				matches = append(matches, key)
			}
		}
		return matches, nil
	}
}

func deleteMockDedupKey(state *mockDedupCacheState, key string) bool {
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
	return existed
}

func configureMockDedupHashCache(client *cachemocks.Client, state *mockDedupCacheState) {
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
		maps.Copy(copied, fields)
		return copied, nil
	}
	client.ExpireFunc = func(_ context.Context, _ string, _ time.Duration) error {
		return nil
	}
}

func configureMockDedupStringCache(client *cachemocks.Client, state *mockDedupCacheState) {
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

	for i := range constants.LocalFallbackCleanupMaxKeys {
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

func TestService_TryClaimNotification_ClaimKeyCategoryAndSchedulePolicy(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 9, 15, 5, 0, time.UTC)

	keyTarget, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildNotifyClaimKey("room1", "vid1", start, "target"), keyTarget)

	keyTargetAgain, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start, 3)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, keyTarget, keyTargetAgain)

	keySameMinute, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start.Add(30*time.Second), 5)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, keyTarget, keySameMinute)

	keyNonTarget, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start, 10)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildNotifyClaimKey("room1", "vid1", start, "10"), keyNonTarget)
	assert.NotEqual(t, keyTarget, keyNonTarget)

	keyDifferentSchedule, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start.Add(time.Minute), 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.NotEqual(t, keyTarget, keyDifferentSchedule)
}

func TestService_TryClaimLogicalEventAndScheduleTransition(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "stream1",
		Title:          "테스트 방송",
		StartScheduled: &start,
	}

	logicalKey, acquired, err := service.TryClaimLogicalEvent(t.Context(), "room1", "UC_TEST", stream, 5)
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildLogicalEventClaimKey("room1", "UC_TEST", stream.ID, stream.Title, start, "target"), logicalKey)

	logicalKeyAgain, acquired, err := service.TryClaimLogicalEvent(t.Context(), "room1", "UC_TEST", stream, 3)
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, logicalKey, logicalKeyAgain)

	transitionKey, acquired, err := service.TryClaimScheduleTransition(t.Context(), stream.ID, start, start.Add(30*time.Minute))
	require.NoError(t, err)
	assert.True(t, acquired)
	assert.Equal(t, keys.BuildScheduleTransitionKey(stream.ID, start, start.Add(30*time.Minute)), transitionKey)

	transitionKeyAgain, acquired, err := service.TryClaimScheduleTransition(t.Context(), stream.ID, start, start.Add(30*time.Minute))
	require.NoError(t, err)
	assert.False(t, acquired)
	assert.Equal(t, transitionKey, transitionKeyAgain)
}

func TestService_DetectScheduleChange(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "stream-schedule-change"
	start := time.Date(2026, 3, 4, 9, 30, 45, 0, time.UTC)
	delayed := time.Date(2026, 3, 4, 9, 45, 12, 0, time.UTC)
	early := time.Date(2026, 3, 4, 9, 15, 33, 0, time.UTC)

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, start, 5))

	message, err := service.DetectScheduleChange(t.Context(), streamID, delayed)
	require.NoError(t, err)
	assert.Equal(t, "일정이 늦춰졌습니다.", message)

	message, err = service.DetectScheduleChange(t.Context(), streamID, delayed)
	require.NoError(t, err)
	assert.Equal(t, "일정이 늦춰졌습니다.", message, "감지는 claim 없이 반복 가능해야 발행 실패 후 재시도할 수 있음")

	message, err = service.DetectScheduleChange(t.Context(), streamID, early)
	require.NoError(t, err)
	assert.Equal(t, "일정이 앞당겨졌습니다.", message)

	message, err = service.DetectScheduleChange(t.Context(), streamID, start.Add(10*time.Second))
	require.NoError(t, err)
	assert.Empty(t, message, "분 단위가 같으면 변경으로 보지 않음")
}

func TestService_DetectNotificationScheduleChange_NoLegacyScanFallback(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	var scanCalls int
	cacheMock.ScanKeysFunc = func(_ context.Context, _ string, _ int64) ([]string, error) {
		scanCalls++
		return nil, nil
	}

	currentScheduled := time.Date(2026, 3, 4, 9, 45, 0, 0, time.UTC)
	currentStream := &domain.Stream{
		ID:             "new-waiting-room",
		Title:          "same title",
		StartScheduled: &currentScheduled,
	}

	change, err := service.DetectNotificationScheduleChange(t.Context(), "room-1", "UC_TEST", currentStream)
	require.NoError(t, err)
	assert.Nil(t, change)
	assert.Equal(t, 0, scanCalls, "DetectNotificationScheduleChange must not fall back to wildcard SCAN")
}

func TestService_DetectNotificationScheduleChange_LogicalWaitingRoomReplacement(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	previousScheduled := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	currentScheduled := time.Date(2026, 3, 4, 9, 45, 0, 0, time.UTC)
	previousStream := &domain.Stream{
		ID:             "old-waiting-room",
		Title:          "same title",
		StartScheduled: &previousScheduled,
	}
	currentStream := &domain.Stream{
		ID:             "new-waiting-room",
		Title:          "same title",
		StartScheduled: &currentScheduled,
	}

	require.NoError(t, service.MarkUpcomingEventNotified(t.Context(), "room-1", "UC_TEST", previousStream))

	change, err := service.DetectNotificationScheduleChange(t.Context(), "room-1", "UC_TEST", currentStream)
	require.NoError(t, err)
	require.NotNil(t, change)
	assert.Equal(t, "일정이 늦춰졌습니다.", change.Message)
	assert.Equal(t, keys.FormatScheduled(previousScheduled), change.PreviousScheduledString())

	change, err = service.DetectNotificationScheduleChange(t.Context(), "room-2", "UC_TEST", currentStream)
	require.NoError(t, err)
	assert.Nil(t, change, "이전 알림을 받지 않은 방에는 교체 지연을 만들지 않음")
}

func TestService_TryClaimNotificationScheduleChange_PerRoomDedup(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	delayed := time.Date(2026, 3, 4, 9, 45, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "new-waiting-room",
		Title:          "same title",
		StartScheduled: &delayed,
	}

	claimKeys, claimed, err := service.TryClaimNotificationScheduleChange(t.Context(), "room-1", "UC_TEST", stream, keys.FormatScheduled(start))
	require.NoError(t, err)
	assert.True(t, claimed)
	assert.Len(t, claimKeys, 2)

	_, claimed, err = service.TryClaimNotificationScheduleChange(t.Context(), "room-1", "UC_TEST", stream, keys.FormatScheduled(start))
	require.NoError(t, err)
	assert.False(t, claimed)

	_, claimed, err = service.TryClaimNotificationScheduleChange(t.Context(), "room-2", "UC_TEST", stream, keys.FormatScheduled(start))
	require.NoError(t, err)
	assert.True(t, claimed, "다른 방에는 같은 지연 전환을 독립적으로 보낼 수 있어야 함")
}

func TestService_MarkAsNotified_TargetMinutePolicyAndScheduleReset(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "vid-notified"
	start := time.Date(2026, 3, 4, 10, 0, 12, 0, time.UTC)

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, start, 5))

	already, err := service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 3)
	require.NoError(t, err)
	assert.True(t, already, "target 분은 같은 스케줄에서 1회만 허용")

	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 10)
	require.NoError(t, err)
	assert.False(t, already, "non-target 분은 개별 분 기준")

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, start, 10))
	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 10)
	require.NoError(t, err)
	assert.True(t, already)

	anyNotified, err := service.IsAlreadyNotified(t.Context(), streamID)
	require.NoError(t, err)
	assert.True(t, anyNotified)

	changed := start.Add(2 * time.Hour)
	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, changed, 5)
	require.NoError(t, err)
	assert.False(t, already, "스케줄이 바뀌면 이전 이력은 무시")

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, changed, 5))

	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 5)
	require.NoError(t, err)
	assert.False(t, already, "새 스케줄 기록 후 기존 스케줄은 차단되지 않아야 함")

	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, changed, 5)
	require.NoError(t, err)
	assert.True(t, already)
}

func TestService_UpdateTargetMinutes_AffectsTargetPolicy(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "vid-dynamic-targets"
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, start, 10))

	already, err := service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 3)
	require.NoError(t, err)
	assert.False(t, already, "10분은 아직 target이 아니라서 3분을 막으면 안 됨")

	service.UpdateTargetMinutes([]int{10, 3, 1})

	already, err = service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 3)
	require.NoError(t, err)
	assert.True(t, already, "10분이 target으로 승격되면 같은 스케줄의 3분 target은 차단돼야 함")
}

func TestIsAlreadyNotified_PropagatesCacheError(t *testing.T) {
	t.Parallel()

	cacheErr := fmt.Errorf("connection refused")
	mockCache, _ := newMockDedupCache(t)
	mockCache.HGetAllFunc = func(_ context.Context, _ string) (map[string]string, error) {
		return nil, cacheErr
	}

	service := NewService(mockCache, []int{5, 3, 1}, newTestLogger())

	_, err := service.IsAlreadyNotified(t.Context(), "stream-error-test")
	if err == nil {
		t.Fatal("expected error from IsAlreadyNotified when cache fails, got nil")
	}
}

func TestService_RecentlyNotifiedStreamIDs(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	require.NoError(t, service.MarkAsNotified(t.Context(), "stream-1", start, 5))

	recent, err := service.RecentlyNotifiedStreamIDs(t.Context(), []string{"stream-1", "stream-2", "stream-1", ""})
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"stream-1": {}}, recent)
}

func TestService_LegacyStringNotifiedData_MigratesToHash(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	streamID := "vid-legacy"
	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	key := keys.NotifiedKey(streamID)

	legacyJSON, err := json.Marshal(NotifiedData{
		StartScheduled: keys.FormatScheduled(start),
		SentAt:         map[int]bool{5: true},
	})
	require.NoError(t, err)
	state.setRawString(key, string(legacyJSON))

	already, err := service.IsAlreadyNotifiedForSchedule(t.Context(), streamID, start, 5)
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

	require.NoError(t, service.MarkAsNotified(t.Context(), streamID, start, 3))

	state.mu.Lock()
	hashFields = maps.Clone(state.hashes[key])
	state.mu.Unlock()

	assert.Equal(t, "1", hashFields["3"])
	assert.Equal(t, "1", hashFields["5"])
}

func TestService_UpcomingEventRecentlyWindow(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "vid-upcoming",
		Title:          "Upcoming",
		StartScheduled: &start,
	}

	require.NoError(t, service.MarkUpcomingEventNotified(t.Context(), "room1", "UC_TEST", stream))

	recent, err := service.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
	require.NoError(t, err)
	assert.True(t, recent)

	key := keys.BuildUpcomingEventKey("room1", "UC_TEST", stream.ID, stream.Title, start)
	stale := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	staleJSON, err := json.Marshal(stale)
	require.NoError(t, err)
	state.setRawString(key, string(staleJSON))

	recent, err = service.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
	require.NoError(t, err)
	assert.False(t, recent)
}

func TestService_WasUpcomingEventNotifiedRecently_InvalidPayloadReturnsError(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 11, 0, 0, 0, time.UTC)
	stream := &domain.Stream{
		ID:             "vid-upcoming",
		Title:          "Upcoming",
		StartScheduled: &start,
	}
	key := keys.BuildUpcomingEventKey("room1", "UC_TEST", stream.ID, stream.Title, start)
	state.setRawString(key, "{invalid-json")

	_, err := service.WasUpcomingEventNotifiedRecently(t.Context(), "room1", "UC_TEST", stream, 15*time.Minute)
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
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	start := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

	key, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.True(t, acquired)

	_, acquired, err = service.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
	require.NoError(t, err)
	assert.False(t, acquired, "SETNX 실패 시에도 로컬 폴백으로 중복 차단돼야 함")

	require.NoError(t, service.ReleaseClaims(t.Context(), []string{key}))

	_, acquired, err = service.TryClaimNotification(t.Context(), "room1", "vid1", start, 5)
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
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	err := service.ReleaseClaims(t.Context(), []string{"claim:key"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "release claims: del many keys")
	assert.ErrorIs(t, err, expectedErr)
}

func TestService_TryClaimNotification_ZeroTime(t *testing.T) {
	service := NewService(nil, []int{5, 3, 1}, newTestLogger())
	key, acquired, err := service.TryClaimNotification(t.Context(), "room1", "vid1", time.Time{}, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_TryClaimLogicalEvent_NilSchedule(t *testing.T) {
	service := NewService(nil, []int{5, 3, 1}, newTestLogger())
	stream := &domain.Stream{ID: "vid1", Title: "test"}
	key, acquired, err := service.TryClaimLogicalEvent(t.Context(), "room1", "UC_A", stream, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_TryClaimLogicalEvent_NilStream(t *testing.T) {
	service := NewService(nil, []int{5, 3, 1}, newTestLogger())
	key, acquired, err := service.TryClaimLogicalEvent(t.Context(), "room1", "UC_A", nil, 5)
	require.NoError(t, err)
	assert.Empty(t, key)
	assert.False(t, acquired)
}

func TestService_ReadNotifiedData_LegacyJSONMigrated(t *testing.T) {
	cacheMock, state := newMockDedupCache(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

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

	notified, err := service.IsAlreadyNotifiedForSchedule(
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

func newMockDedupCacheWithSetNXMulti(t *testing.T) (*cachemocks.Client, *mockDedupCacheState) {
	t.Helper()
	client, state := newMockDedupCache(t)
	client.SetNXMultiFunc = func(_ context.Context, entries []cache.SetNXEntry) ([]cache.SetNXResult, error) {
		state.mu.Lock()
		defer state.mu.Unlock()

		now := state.now()
		results := make([]cache.SetNXResult, len(entries))
		for i, e := range entries {
			if expiresAt, exists := state.setNX[e.Key]; exists && now.Before(expiresAt) {
				results[i] = cache.SetNXResult{Key: e.Key, Acquired: false}
				continue
			}
			if e.TTL <= 0 {
				state.setNX[e.Key] = now
			} else {
				state.setNX[e.Key] = now.Add(e.TTL)
			}
			results[i] = cache.SetNXResult{Key: e.Key, Acquired: true}
		}
		return results, nil
	}
	return client, state
}

func TestService_TryClaimPair_BothAcquired(t *testing.T) {
	cacheMock, _ := newMockDedupCacheWithSetNXMulti(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	a1, a2 := service.TryClaimPair(t.Context(), "pair:k1", "pair:k2", 5*time.Minute)
	assert.True(t, a1)
	assert.True(t, a2)
}

func TestService_TryClaimPair_Key1AcquiredKey2Exists(t *testing.T) {
	cacheMock, state := newMockDedupCacheWithSetNXMulti(t)
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	state.mu.Lock()
	state.setNX["pair:k2"] = state.now().Add(10 * time.Minute)
	state.mu.Unlock()

	a1, a2 := service.TryClaimPair(t.Context(), "pair:k1", "pair:k2", 5*time.Minute)
	assert.True(t, a1)
	assert.False(t, a2)
}

func TestService_TryClaimPair_SetNXMultiError_Fallback(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	cacheMock.SetNXMultiFunc = func(_ context.Context, _ []cache.SetNXEntry) ([]cache.SetNXResult, error) {
		return nil, errors.New("pipeline broken")
	}
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	a1, a2 := service.TryClaimPair(t.Context(), "fb:k1", "fb:k2", 5*time.Minute)
	assert.True(t, a1, "fallback grants first claim")
	assert.True(t, a2, "fallback grants first claim")

	a1, a2 = service.TryClaimPair(t.Context(), "fb:k1", "fb:k2", 5*time.Minute)
	assert.False(t, a1, "fallback dedup blocks second claim")
	assert.False(t, a2, "fallback dedup blocks second claim")
}

func TestService_TryClaimPair_NilSetNXMultiResults_Fallback(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	cacheMock.SetNXMultiFunc = func(_ context.Context, _ []cache.SetNXEntry) ([]cache.SetNXResult, error) {
		return nil, nil
	}
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	a1, a2 := service.TryClaimPair(t.Context(), "nil:k1", "nil:k2", 5*time.Minute)
	assert.True(t, a1, "nil pipeline result falls back and grants first claim")
	assert.True(t, a2, "nil pipeline result falls back and grants first claim")

	a1, a2 = service.TryClaimPair(t.Context(), "nil:k1", "nil:k2", 5*time.Minute)
	assert.False(t, a1, "fallback dedup blocks second claim")
	assert.False(t, a2, "fallback dedup blocks second claim")
}

func TestService_TryClaimPair_PerKeyError_Fallback(t *testing.T) {
	cacheMock, _ := newMockDedupCache(t)
	cacheMock.SetNXMultiFunc = func(_ context.Context, entries []cache.SetNXEntry) ([]cache.SetNXResult, error) {
		results := make([]cache.SetNXResult, len(entries))
		results[0] = cache.SetNXResult{Key: entries[0].Key, Acquired: true}
		results[1] = cache.SetNXResult{Key: entries[1].Key, Err: errors.New("key2 error")}
		return results, nil
	}
	service := NewService(cacheMock, []int{5, 3, 1}, newTestLogger())

	a1, a2 := service.TryClaimPair(t.Context(), "pk:k1", "pk:k2", 5*time.Minute)
	assert.True(t, a1, "key1 acquired from pipeline result")
	assert.True(t, a2, "key2 falls back and grants first claim")

	a1Again, a2Again := service.TryClaimPair(t.Context(), "pk:k1", "pk:k2", 5*time.Minute)
	assert.True(t, a1Again, "key1 still acquired from pipeline (mock always returns true)")
	assert.False(t, a2Again, "key2 fallback dedup blocks second claim")
}

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
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

type LogicalScheduleNotifiedData struct {
	StreamID       string `json:"stream_id"`
	StartScheduled string `json:"start_scheduled"`
	NotifiedAt     string `json:"notified_at"`
}

type Service struct {
	cache           cache.Client
	targetPolicy    sharedchecker.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	fallback        *LocalFallback
	logger          *slog.Logger
}

type notifiedDataSource int

const (
	notifiedDataSourceMissing notifiedDataSource = iota
	notifiedDataSourceHash
	notifiedDataSourceLegacyString
)

func NewService(c cache.Client, targetMinutes []int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cache:        c,
		targetPolicy: sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes)),
		fallback:     NewLocalFallback(logger),
		logger:       logger,
	}
}

// UpdateTargetMinutes는 runtime target minute 정책을 원자적으로 교체한다.
func (s *Service) UpdateTargetMinutes(targetMinutes []int) {
	s.targetMinutesMu.Lock()
	defer s.targetMinutesMu.Unlock()

	s.targetPolicy = sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes))
}

// startScheduled가 zero이면 ("", false, nil) 반환
func (s *Service) TryClaimNotification(ctx context.Context, roomID, streamID string, startScheduled time.Time, minutesUntil int) (string, bool, error) {
	if startScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutesSnapshot(), minutesUntil)
	key := keys.BuildNotifyClaimKey(roomID, streamID, startScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimLogicalEvent(ctx context.Context, roomID, channelID string, stream *domain.Stream, minutesUntil int) (string, bool, error) {
	if stream == nil {
		return "", false, nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutesSnapshot(), minutesUntil)
	key := keys.BuildLogicalEventClaimKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimPair(ctx context.Context, key1, key2 string, ttl time.Duration) (acquired1, acquired2 bool) {
	results, err := s.cache.SetNXMulti(ctx, []cache.SetNXEntry{
		{Key: key1, Value: "1", TTL: ttl},
		{Key: key2, Value: "1", TTL: ttl},
	})
	if err != nil {
		return s.fallback.TryClaimOnOutage(key1, ttl, err),
			s.fallback.TryClaimOnOutage(key2, ttl, err)
	}
	return s.resolveClaimResult(key1, ttl, results[0]),
		s.resolveClaimResult(key2, ttl, results[1])
}

func (s *Service) resolveClaimResult(key string, ttl time.Duration, r cache.SetNXResult) bool {
	if r.Err != nil {
		return s.fallback.TryClaimOnOutage(key, ttl, r.Err)
	}
	return r.Acquired
}

func (s *Service) TryClaimScheduleTransition(ctx context.Context, streamID string, oldScheduled, newScheduled time.Time) (string, bool, error) {
	key := keys.BuildScheduleTransitionKey(streamID, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimRoomScheduleTransition(ctx context.Context, roomID, streamID string, oldScheduled, newScheduled time.Time) (string, bool, error) {
	key := keys.BuildRoomScheduleTransitionKey(roomID, streamID, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimLogicalScheduleTransition(ctx context.Context, roomID, channelID string, stream *domain.Stream, oldScheduled, newScheduled time.Time) (string, bool, error) {
	if stream == nil {
		return "", false, nil
	}

	key := keys.BuildLogicalScheduleTransitionKey(roomID, channelID, stream.ID, stream.Title, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) ReleaseClaims(ctx context.Context, claimKeys []string) error {
	if len(claimKeys) == 0 {
		return nil
	}
	s.fallback.ReleaseClaims(claimKeys)

	_, err := s.cache.DelMany(ctx, claimKeys)
	if err != nil {
		return fmt.Errorf("release claims: del many keys: %w", err)
	}
	return nil
}

func (s *Service) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	existing, source, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return fmt.Errorf("mark as notified: load existing data: %w", err)
	}

	existing, source, err = s.resetStaleNotifiedData(ctx, key, existing, source, scheduledStr)
	if err != nil {
		return err
	}

	if source == notifiedDataSourceLegacyString {
		return s.markLegacyNotified(ctx, key, existing, scheduledStr, minutesUntil)
	}

	return s.writeNotifiedHashFields(ctx, key, scheduledStr, minutesUntil)
}

func (s *Service) resetStaleNotifiedData(
	ctx context.Context,
	key string,
	existing *NotifiedData,
	source notifiedDataSource,
	scheduledStr string,
) (*NotifiedData, notifiedDataSource, error) {
	if existing == nil || existing.StartScheduled == "" || existing.StartScheduled == scheduledStr {
		return existing, source, nil
	}

	if err := s.cache.Del(ctx, key); err != nil {
		return nil, source, fmt.Errorf("mark as notified: reset old schedule hash: %w", err)
	}
	return nil, notifiedDataSourceMissing, nil
}

func (s *Service) markLegacyNotified(ctx context.Context, key string, existing *NotifiedData, scheduledStr string, minutesUntil int) error {
	if existing == nil {
		existing = &NotifiedData{}
	}
	if existing.SentAt == nil {
		existing.SentAt = make(map[int]bool)
	}
	existing.StartScheduled = scheduledStr
	existing.SentAt[minutesUntil] = true
	if err := s.migrateLegacyNotifiedData(ctx, key, existing); err != nil {
		return fmt.Errorf("mark as notified: migrate legacy data: %w", err)
	}
	return nil
}

func (s *Service) writeNotifiedHashFields(ctx context.Context, key string, scheduledStr string, minutesUntil int) error {
	fields := map[string]any{
		"start_scheduled":          scheduledStr,
		strconv.Itoa(minutesUntil): "1",
	}
	if err := s.cache.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("mark as notified: hmset fields: %w", err)
	}
	if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark as notified: set expiration: %w", err)
	}
	return nil
}

func (s *Service) IsAlreadyNotifiedForSchedule(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) (bool, error) {
	key := keys.NotifiedKey(streamID)
	scheduledStr := keys.FormatScheduled(startScheduled)

	data, err := s.readNotifiedData(ctx, key)
	if err != nil {
		return false, fmt.Errorf("is already notified for schedule: %w", err)
	}
	if data == nil {
		return false, nil
	}

	// 스케줄 변경됨 -> 발송 허용
	if data.StartScheduled != scheduledStr {
		return false, nil
	}

	// live catchup: SentAt[0] 확인
	if minutesUntil == 0 {
		return data.SentAt[0], nil
	}

	targetPolicy := s.targetPolicySnapshot()
	targetMinutes := targetPolicy.Clone()

	if targetPolicy.Contains(minutesUntil) {
		return targetMinuteAlreadySent(data.SentAt, targetMinutes), nil
	}

	// non-target: 해당 분만 확인
	return data.SentAt[minutesUntil], nil
}

func targetMinuteAlreadySent(sentAt map[int]bool, targetMinutes []int) bool {
	for _, target := range targetMinutes {
		if sentAt[target] {
			return true
		}
	}
	return false
}

func (s *Service) IsAlreadyNotified(ctx context.Context, streamID string) (bool, error) {
	key := keys.NotifiedKey(streamID)
	data, err := s.readNotifiedData(ctx, key)
	if err != nil {
		return false, fmt.Errorf("is already notified: %w", err)
	}
	if data == nil {
		return false, nil
	}
	return len(data.SentAt) > 0, nil
}

func (s *Service) RecentlyNotifiedStreamIDs(ctx context.Context, streamIDs []string) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if s == nil || len(streamIDs) == 0 {
		return result, nil
	}

	for _, streamID := range uniqueNonEmptyStrings(streamIDs) {
		data, err := s.readNotifiedData(ctx, keys.NotifiedKey(streamID))
		if err != nil {
			return nil, fmt.Errorf("recently notified stream ids: read %s: %w", streamID, err)
		}
		if data == nil || len(data.SentAt) == 0 {
			continue
		}
		result[streamID] = struct{}{}
	}

	return result, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Service) MarkUpcomingEventNotified(ctx context.Context, roomID, channelID string, stream *domain.Stream) error {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)
	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark upcoming event notified: set cache key: %w", err)
	}
	if err := s.MarkLogicalScheduleObserved(ctx, roomID, channelID, stream); err != nil {
		return fmt.Errorf("mark upcoming event notified: mark logical schedule observed: %w", err)
	}
	return nil
}

func (s *Service) WasUpcomingEventNotifiedRecently(ctx context.Context, roomID, channelID string, stream *domain.Stream, window time.Duration) (bool, error) {
	if stream == nil {
		return false, nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return false, nil
	}
	if window <= 0 {
		return false, nil
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data UpcomingEventNotifiedData
	if err := s.cache.Get(ctx, key, &data); err != nil {
		return false, fmt.Errorf("was upcoming event notified recently: get cache data: %w", err)
	}
	if data.NotifiedAt == "" {
		return false, nil
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false, nil
	}

	return time.Since(notifiedAt) <= window, nil
}

// tryClaimKey: SETNX 기반 키 선점 (Valkey 장애 시 로컬 폴백)
func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) bool {
	acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
	if err != nil {
		s.logger.Debug("dedup claim fallback",
			slog.String("key", key),
			slog.String("error", err.Error()),
		)
		return s.fallback.TryClaimOnOutage(key, ttl, err)
	}
	s.logger.Debug("dedup claim result",
		slog.String("key", key),
		slog.Bool("acquired", acquired),
	)
	return acquired
}

func (s *Service) targetMinutesSnapshot() []int {
	return s.targetPolicySnapshot().Clone()
}

func (s *Service) TargetMinutesSnapshot() []int {
	return s.targetMinutesSnapshot()
}

func (s *Service) targetPolicySnapshot() sharedchecker.TargetMinutePolicy {
	s.targetMinutesMu.RLock()
	defer s.targetMinutesMu.RUnlock()

	return s.targetPolicy
}

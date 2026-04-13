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
	"slices"
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

type Service struct {
	cache           cache.Client
	targetMinutes   []int
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
		cache:         c,
		targetMinutes: sharedchecker.NormalizeTargetMinutes(targetMinutes),
		fallback:      NewLocalFallback(logger),
		logger:        logger,
	}
}

// UpdateTargetMinutes는 runtime target minute 정책을 원자적으로 교체한다.
func (s *Service) UpdateTargetMinutes(targetMinutes []int) {
	s.targetMinutesMu.Lock()
	defer s.targetMinutesMu.Unlock()

	s.targetMinutes = sharedchecker.NormalizeTargetMinutes(targetMinutes)
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

func (s *Service) TryClaimScheduleTransition(ctx context.Context, streamID string, oldScheduled, newScheduled time.Time) (string, bool, error) {
	key := keys.BuildScheduleTransitionKey(streamID, oldScheduled, newScheduled)
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

	if existing != nil && existing.StartScheduled != "" && existing.StartScheduled != scheduledStr {
		if err := s.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("mark as notified: reset old schedule hash: %w", err)
		}
		existing = nil
		source = notifiedDataSourceMissing
	}

	if source == notifiedDataSourceLegacyString {
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

	if err := s.cache.HSet(ctx, key, "start_scheduled", scheduledStr); err != nil {
		return fmt.Errorf("mark as notified: set start_scheduled field: %w", err)
	}
	if err := s.cache.HSet(ctx, key, strconv.Itoa(minutesUntil), "1"); err != nil {
		return fmt.Errorf("mark as notified: set minute field: %w", err)
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

	targetMinutes := s.targetMinutesSnapshot()

	// target 분: 어떤 target이라도 발송됐으면 차단 (1회 정책)
	if slices.Contains(targetMinutes, minutesUntil) {
		for _, target := range targetMinutes {
			if data.SentAt[target] {
				return true, nil
			}
		}
		return false, nil
	}

	// non-target: 해당 분만 확인
	return data.SentAt[minutesUntil], nil
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
		return s.fallback.TryClaimOnOutage(key, ttl, err)
	}
	return acquired
}

func (s *Service) targetMinutesSnapshot() []int {
	s.targetMinutesMu.RLock()
	defer s.targetMinutesMu.RUnlock()

	return append([]int(nil), s.targetMinutes...)
}

func (s *Service) readNotifiedData(ctx context.Context, key string) (*NotifiedData, error) {
	data, source, err := s.loadNotifiedData(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified data: load notified data: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	if source == notifiedDataSourceLegacyString {
		if migErr := s.migrateLegacyNotifiedData(ctx, key, data); migErr != nil {
			s.logger.Warn("Failed to migrate legacy notified cache",
				slog.String("key", key),
				slog.Any("error", migErr),
			)
		}
	}

	return data, nil
}

func (s *Service) loadNotifiedData(ctx context.Context, key string) (*NotifiedData, notifiedDataSource, error) {
	fields, err := s.readNotifiedHashFields(ctx, key)
	if err == nil {
		if len(fields) == 0 {
			return nil, notifiedDataSourceMissing, nil
		}
		return parseNotifiedHash(fields), notifiedDataSourceHash, nil
	}
	if !isWrongTypeError(err) {
		return nil, notifiedDataSourceMissing, err
	}

	var legacy NotifiedData
	if err := s.cache.Get(ctx, key, &legacy); err != nil {
		return nil, notifiedDataSourceMissing, fmt.Errorf("get legacy string: %w", err)
	}
	if legacy.StartScheduled == "" && len(legacy.SentAt) == 0 {
		return nil, notifiedDataSourceMissing, nil
	}
	if legacy.SentAt == nil {
		legacy.SentAt = make(map[int]bool)
	}

	return &legacy, notifiedDataSourceLegacyString, nil
}

func (s *Service) readNotifiedHashFields(ctx context.Context, key string) (map[string]string, error) {
	fields, err := s.cache.HGetAll(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read notified hash fields: %w", err)
	}
	return fields, nil
}

func (s *Service) migrateLegacyNotifiedData(ctx context.Context, key string, data *NotifiedData) error {
	if err := s.cache.Del(ctx, key); err != nil {
		return fmt.Errorf("delete legacy key: %w", err)
	}
	if err := s.persistNotifiedHash(ctx, key, data); err != nil {
		return fmt.Errorf("persist migrated hash: %w", err)
	}
	return nil
}

func (s *Service) persistNotifiedHash(ctx context.Context, key string, data *NotifiedData) error {
	fields := make(map[string]any, len(data.SentAt)+1)
	fields["start_scheduled"] = data.StartScheduled
	for minute, sent := range data.SentAt {
		if !sent {
			continue
		}
		fields[strconv.Itoa(minute)] = "1"
	}
	if err := s.cache.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("hmset notified hash: %w", err)
	}
	if err := s.cache.Expire(ctx, key, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("expire notified hash: %w", err)
	}
	return nil
}

func parseNotifiedHash(fields map[string]string) *NotifiedData {
	startScheduled := fields["start_scheduled"]
	sentAt := make(map[int]bool)
	for k := range fields {
		if k == "start_scheduled" {
			continue
		}
		if m, err := strconv.Atoi(k); err == nil {
			sentAt[m] = true
		}
	}

	return &NotifiedData{
		StartScheduled: startScheduled,
		SentAt:         sentAt,
	}
}

func isWrongTypeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "WRONGTYPE")
}

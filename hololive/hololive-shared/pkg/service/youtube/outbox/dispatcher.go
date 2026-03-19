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

package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// Config: Dispatcher 설정
type Config struct {
	BatchSize           int           // 한 번에 처리할 알림 수
	LockTimeout         time.Duration // 락 타임아웃 (처리 중 상태 유지 시간)
	PollInterval        time.Duration // 폴링 간격
	MaxRetries          int           // 최대 재시도 횟수
	RetryBackoff        time.Duration // 재시도 간격
	CleanupAfter        time.Duration // 완료된 알림 정리 기간
	CleanupEnabled      bool          // 정리 활성화 여부
	DeliveryParallelism int           // room/delivery send 제한 병렬성
}

// DefaultConfig: 기본 설정
func DefaultConfig() Config {
	return Config{
		BatchSize:           50,
		LockTimeout:         5 * time.Minute,
		PollInterval:        30 * time.Second,
		MaxRetries:          3,
		RetryBackoff:        1 * time.Minute,
		CleanupAfter:        7 * 24 * time.Hour, // 7일
		CleanupEnabled:      true,
		DeliveryParallelism: 4,
	}
}

// Dispatcher: Outbox 알림 발송 처리기
type Dispatcher struct {
	db        *gorm.DB
	cache     cache.Client
	sender    iris.Client
	renderer  *template.Renderer
	logger    *slog.Logger
	cfg       Config
	delivery  *DeliveryRepository
	formatter *MessageFormatter
}

// NewDispatcher: 새 Dispatcher 생성
func NewDispatcher(db *gorm.DB, cacheSvc cache.Client, sender iris.Client, renderer *template.Renderer, logger *slog.Logger, cfg Config) *Dispatcher {
	initOutboxMetrics()

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultConfig().BatchSize
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = DefaultConfig().LockTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultConfig().PollInterval
	}

	return &Dispatcher{
		db:       db,
		cache:    cacheSvc,
		sender:   sender,
		renderer: renderer,
		logger:   logger,
		cfg:      cfg,
		delivery: NewDeliveryRepository(db, logger),
		formatter: &MessageFormatter{
			renderer: renderer,
			cache:    cacheSvc,
			logger:   logger,
		},
	}
}

// Start: 백그라운드 폴링 루프 시작
func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
	if d.cfg.CleanupEnabled {
		go d.cleanupLoop(ctx)
	}
}

// run: 메인 폴링 루프
func (d *Dispatcher) run(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.cfg.PollInterval),
		slog.Int("batch_size", d.cfg.BatchSize))

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("Outbox dispatcher stopped")
			return
		case <-ticker.C:
			d.processOnce(ctx)
		}
	}
}

// processOnce: 한 번의 폴링 사이클
func (d *Dispatcher) processOnce(ctx context.Context) {
	outboxItems, err := d.claimOutboxBatch(ctx)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
		return
	}

	if len(outboxItems) == 0 {
		return
	}

	d.logger.Debug("Processing outbox batch", slog.Int("count", len(outboxItems)))
	d.processPerRoomBatch(ctx, outboxItems)
}

// cleanupLoop: 오래된 완료 알림 정리 루프
func (d *Dispatcher) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.cleanup(ctx)
		}
	}
}

// cleanup: 오래된 완료 알림 삭제
func (d *Dispatcher) cleanup(ctx context.Context) {
	cutoff := time.Now().Add(-d.cfg.CleanupAfter)

	result := d.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.OutboxStatusSent, domain.OutboxStatusFailed, cutoff).
		Delete(&domain.YouTubeNotificationOutbox{})

	if result.Error != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", result.Error))
		return
	}

	if result.RowsAffected > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", result.RowsAffected))
	}
}

// outboxItemGroup: Outbox 알림 그룹 (동일 Room + Channel + Kind 묶음)
type outboxItemGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	items     []domain.YouTubeNotificationOutbox
}

func (d *Dispatcher) groupOutboxItems(items []domain.YouTubeNotificationOutbox, roomsByChannel map[string]map[string]bool) []*outboxItemGroup {
	if len(items) == 0 {
		return nil
	}

	groups := make([]*outboxItemGroup, 0)
	index := make(map[string]int)

	for i := range items {
		item := &items[i]
		rooms, ok := roomsByChannel[item.ChannelID]
		if !ok || len(rooms) == 0 {
			continue
		}

		for roomID := range rooms {
			key := fmt.Sprintf("%s|%s|%s", roomID, item.ChannelID, item.Kind)
			if idx, exists := index[key]; exists {
				groups[idx].items = append(groups[idx].items, *item)
				continue
			}

			groups = append(groups, &outboxItemGroup{
				roomID:    roomID,
				channelID: item.ChannelID,
				kind:      item.Kind,
				items:     []domain.YouTubeNotificationOutbox{*item},
			})
			index[key] = len(groups) - 1
		}
	}

	return groups
}

func (d *Dispatcher) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]map[string]bool {
	result := make(map[string]map[string]bool)

	// 고유 채널 ID 추출 + 키 생성
	type channelEntry struct {
		channelID string
		key       string
	}
	var entries []channelEntry
	seen := make(map[string]bool)
	for i := range items {
		item := &items[i]
		if seen[item.ChannelID] {
			continue
		}
		seen[item.ChannelID] = true
		key := channelSubscribersKey(item.ChannelID, item.Kind.ToAlarmType())
		entries = append(entries, channelEntry{channelID: item.ChannelID, key: key})
	}

	if len(entries) == 0 {
		return result
	}

	// DoMulti pipeline: 모든 SMEMBERS를 한 번에 실행 (RTT N→1)
	b := d.cache.B()
	cmds := make([]valkey.Completed, len(entries))
	for i, e := range entries {
		cmds[i] = b.Smembers().Key(e.key).Build()
	}
	results := d.cache.DoMulti(ctx, cmds...)

	for i, e := range entries {
		members, err := results[i].AsStrSlice()
		if err != nil {
			d.logger.Warn("Failed to get subscribers for channel",
				slog.String("channel_id", e.channelID),
				slog.Any("error", err))
			continue
		}

		result[e.channelID] = map[string]bool{}
		if len(members) == 0 {
			continue
		}

		roomSet := make(map[string]bool, len(members))
		for _, roomID := range members {
			roomSet[roomID] = true
		}
		result[e.channelID] = roomSet
	}

	return result
}

// channelSubscribersKey: 채널 ID와 알람 타입으로 Valkey 키를 생성한다.
func channelSubscribersKey(channelID string, alarmType domain.AlarmType) string {
	switch alarmType {
	case domain.AlarmTypeCommunity:
		return "alarm:channel_subscribers:COMMUNITY:" + channelID
	case domain.AlarmTypeShorts:
		return "alarm:channel_subscribers:SHORTS:" + channelID
	default:
		return "alarm:channel_subscribers:" + channelID
	}
}

// ProcessOnceForTest: 테스트용 - 한 번의 폴링 사이클 실행
func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}

// CleanupForTest: 테스트용 - 정리 루프 본문 실행
func (d *Dispatcher) CleanupForTest(ctx context.Context) {
	d.cleanup(ctx)
}

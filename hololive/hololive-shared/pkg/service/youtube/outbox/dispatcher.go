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

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

const defaultTelemetryRetention = 24 * time.Hour

// Config: Dispatcher 설정
type Config struct {
	BatchSize              int           // 한 번에 처리할 알림 수
	LockTimeout            time.Duration // 락 타임아웃 (처리 중 상태 유지 시간)
	PollInterval           time.Duration // 폴링 간격
	MaxRetries             int           // 최대 재시도 횟수
	RetryBackoff           time.Duration // 재시도 간격
	CleanupAfter           time.Duration // 완료된 알림 정리 기간
	CleanupEnabled         bool          // 정리 활성화 여부
	DeliveryParallelism    int           // room/delivery send 제한 병렬성
	AggregateSyncInterval  time.Duration // aggregate sync maintenance interval
	TelemetryPollInterval  time.Duration // telemetry loop polling interval
	TelemetryBackfillBatch int           // delivery 상태에서 telemetry 버퍼로 역보강할 최대 건수
	TelemetryFlushBatch    int           // telemetry 버퍼 플러시 최대 건수
	TelemetryRetryBackoff  time.Duration // telemetry 플러시 실패 재시도 간격
	TelemetryRetention     time.Duration // telemetry 버퍼 최소 보존 기간
}

// DefaultConfig: 기본 설정
func DefaultConfig() Config {
	return Config{
		BatchSize:              50,
		LockTimeout:            5 * time.Minute,
		PollInterval:           2 * time.Second,
		MaxRetries:             3,
		RetryBackoff:           1 * time.Minute,
		CleanupAfter:           7 * 24 * time.Hour, // 7일
		CleanupEnabled:         true,
		DeliveryParallelism:    4,
		AggregateSyncInterval:  30 * time.Second,
		TelemetryPollInterval:  30 * time.Second,
		TelemetryBackfillBatch: 200,
		TelemetryFlushBatch:    200,
		TelemetryRetryBackoff:  30 * time.Second,
		TelemetryRetention:     defaultTelemetryRetention,
	}
}

// Dispatcher: Outbox 알림 발송 처리기
type Dispatcher struct {
	db        *gorm.DB
	cache     cache.Client
	sender    delivery.MessageSender
	renderer  *template.Renderer
	logger    *slog.Logger
	cfg       Config
	delivery  *DeliveryRepository
	telemetry *DeliveryTelemetryRepository
	formatter *MessageFormatter
}

// NewDispatcher: 새 Dispatcher 생성
func NewDispatcher(db *gorm.DB, cacheSvc cache.Client, sender delivery.MessageSender, renderer *template.Renderer, logger *slog.Logger, cfg Config) *Dispatcher {
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
	if cfg.TelemetryBackfillBatch <= 0 {
		cfg.TelemetryBackfillBatch = DefaultConfig().TelemetryBackfillBatch
	}
	if cfg.TelemetryPollInterval <= 0 {
		cfg.TelemetryPollInterval = DefaultConfig().TelemetryPollInterval
	}
	if cfg.AggregateSyncInterval <= 0 {
		cfg.AggregateSyncInterval = DefaultConfig().AggregateSyncInterval
	}
	if cfg.TelemetryFlushBatch <= 0 {
		cfg.TelemetryFlushBatch = DefaultConfig().TelemetryFlushBatch
	}
	if cfg.TelemetryRetryBackoff <= 0 {
		cfg.TelemetryRetryBackoff = DefaultConfig().TelemetryRetryBackoff
	}
	if cfg.TelemetryRetention <= 0 {
		cfg.TelemetryRetention = DefaultConfig().TelemetryRetention
	}

	var telemetryRepo *DeliveryTelemetryRepository
	if db != nil {
		telemetryRepo = NewDeliveryTelemetryRepository(db)
	}

	return &Dispatcher{
		db:        db,
		cache:     cacheSvc,
		sender:    sender,
		renderer:  renderer,
		logger:    logger,
		cfg:       cfg,
		delivery:  NewDeliveryRepository(db, logger),
		telemetry: telemetryRepo,
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
	if d.delivery != nil {
		go d.aggregateSyncLoop(ctx)
	}
	if d.telemetry != nil {
		go d.telemetryLoop(ctx)
	}
	if d.cfg.CleanupEnabled {
		go d.cleanupLoop(ctx)
	}
}

func (d *Dispatcher) aggregateSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.AggregateSyncInterval)
	defer ticker.Stop()

	d.reconcileTerminalOutboxStatuses(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.reconcileTerminalOutboxStatuses(ctx)
		}
	}
}

// run: 메인 폴링 루프
func (d *Dispatcher) run(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	d.logger.Info("Outbox dispatcher started",
		slog.Duration("poll_interval", d.cfg.PollInterval),
		slog.Int("batch_size", d.cfg.BatchSize))

	d.processOnce(ctx)

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
	d.processAvailable(ctx, 4)
}

func (d *Dispatcher) processAvailable(ctx context.Context, maxRounds int) {
	if maxRounds <= 0 {
		maxRounds = 1
	}

	for round := 0; round < maxRounds; round++ {
		outboxItems, err := d.claimOutboxBatch(ctx)
		if err != nil {
			d.logger.Error("Failed to fetch outbox items", slog.Any("error", err))
			return
		}

		deliveryCount := 0
		if len(outboxItems) > 0 {
			d.logger.Debug("Processing outbox batch",
				slog.Int("count", len(outboxItems)),
				slog.Int("round", round+1))
			deliveryCount = d.processPerRoomBatch(ctx, outboxItems)
		} else {
			deliveryCount = d.processPendingDeliveries(ctx)
		}

		if len(outboxItems) == 0 && deliveryCount == 0 {
			return
		}
	}
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
	if d == nil || d.db == nil {
		return
	}

	now := time.Now().UTC()
	outboxCutoff := now.Add(-d.cfg.CleanupAfter)

	result := d.db.WithContext(ctx).
		Where("status IN (?, ?) AND COALESCE(sent_at, created_at) < ?", domain.OutboxStatusSent, domain.OutboxStatusFailed, outboxCutoff).
		Delete(&domain.YouTubeNotificationOutbox{})

	if result.Error != nil {
		d.logger.Warn("Failed to cleanup old outbox items", slog.Any("error", result.Error))
		return
	}

	if result.RowsAffected > 0 {
		d.logger.Info("Cleaned up old outbox items", slog.Int64("deleted", result.RowsAffected))
	}

	if d.telemetry == nil || d.cfg.TelemetryRetention <= 0 {
		return
	}

	telemetryCutoff := now.Add(-d.cfg.TelemetryRetention)
	deletedTelemetry, err := d.telemetry.DeleteLoggedBefore(ctx, telemetryCutoff)
	if err != nil {
		d.logger.Warn("Failed to cleanup old delivery telemetry", slog.Any("error", err))
		return
	}

	if deletedTelemetry > 0 {
		d.logger.Info("Cleaned up old delivery telemetry",
			slog.Int64("deleted", deletedTelemetry),
			slog.Duration("retention", d.cfg.TelemetryRetention))
	}
}

// outboxItemGroup: Outbox 알림 그룹 (동일 Room + Channel + Kind 묶음)
type outboxItemGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	items     []domain.YouTubeNotificationOutbox
}

type channelAlarmRoomTargets map[domain.AlarmType]map[string]bool

func roomsForItem(roomsByChannel map[string]channelAlarmRoomTargets, item domain.YouTubeNotificationOutbox) (map[string]bool, bool) {
	alarmTargets, ok := roomsByChannel[item.ChannelID]
	if !ok {
		return nil, false
	}

	rooms, ok := alarmTargets[item.Kind.ToAlarmType()]
	if !ok {
		return nil, false
	}

	return rooms, true
}

func (d *Dispatcher) groupOutboxItems(items []domain.YouTubeNotificationOutbox, roomsByChannel map[string]channelAlarmRoomTargets) []*outboxItemGroup {
	if len(items) == 0 {
		return nil
	}

	groups := make([]*outboxItemGroup, 0)
	index := make(map[string]int)

	for i := range items {
		item := &items[i]
		rooms, ok := roomsForItem(roomsByChannel, *item)
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

func (d *Dispatcher) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	result := make(map[string]channelAlarmRoomTargets)

	// 고유 채널 ID + 알람 타입 추출
	type channelEntry struct {
		channelID string
		alarmType domain.AlarmType
	}
	var entries []channelEntry
	seen := make(map[string]bool)
	for i := range items {
		item := &items[i]
		alarmType := item.Kind.ToAlarmType()
		lookupKey := item.ChannelID + "|" + string(alarmType)
		if seen[lookupKey] {
			continue
		}
		seen[lookupKey] = true
		entries = append(entries, channelEntry{channelID: item.ChannelID, alarmType: alarmType})
	}

	if len(entries) == 0 {
		return result
	}

	type lookupResult struct {
		channelID string
		alarmType domain.AlarmType
		rooms     map[string]bool
		ok        bool
	}

	results := make([]lookupResult, len(entries))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(16)
	for idx := range entries {
		idx := idx
		eg.Go(func() error {
			e := entries[idx]
			members, err := sharedalarm.ResolveChannelSubscribersByType(egCtx, d.cache, d.db, e.channelID, e.alarmType)
			if err != nil {
				d.logger.Warn("Failed to get subscribers for channel",
					slog.String("channel_id", e.channelID),
					slog.String("alarm_type", string(e.alarmType)),
					slog.Any("error", err))
				return nil
			}

			roomSet := make(map[string]bool, len(members))
			for _, roomID := range members {
				roomSet[roomID] = true
			}
			results[idx] = lookupResult{
				channelID: e.channelID,
				alarmType: e.alarmType,
				rooms:     roomSet,
				ok:        true,
			}
			return nil
		})
	}
	_ = eg.Wait()

	for i := range results {
		if !results[i].ok {
			continue
		}

		alarmTargets, ok := result[results[i].channelID]
		if !ok {
			alarmTargets = make(channelAlarmRoomTargets)
			result[results[i].channelID] = alarmTargets
		}
		alarmTargets[results[i].alarmType] = results[i].rooms
	}

	return result
}

// ProcessOnceForTest: 테스트용 - 한 번의 폴링 사이클 실행
func (d *Dispatcher) ProcessOnceForTest(ctx context.Context) {
	d.processOnce(ctx)
}

// CleanupForTest: 테스트용 - 정리 루프 본문 실행
func (d *Dispatcher) CleanupForTest(ctx context.Context) {
	d.cleanup(ctx)
}

// AggregateSyncForTest: 테스트용 - aggregate sync 루프 본문 실행
func (d *Dispatcher) AggregateSyncForTest(ctx context.Context) {
	d.reconcileTerminalOutboxStatuses(ctx)
}

package delivery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// MessageSender: 메시지 발송 인터페이스
type MessageSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

// deliveryRepository: Dispatcher가 사용하는 outbox 저장소 인터페이스 (테스트 mock 용도)
type deliveryRepository interface {
	FetchAndLock(ctx context.Context, batchSize int, lockTimeout time.Duration) ([]domain.NotificationDeliveryOutbox, error)
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, maxRetries int, backoff time.Duration, errMsg string) error
	CountByStatus(ctx context.Context, status domain.DeliveryOutboxStatus) (int64, error)
	Cleanup(ctx context.Context, olderThan time.Duration) (int64, error)
}

// DispatcherConfig: Dispatcher 설정
type DispatcherConfig struct {
	BatchSize       int
	MaxConcurrent   int
	MaxRetries      int
	LockTimeout     time.Duration
	PollInterval    time.Duration
	RetryBackoff    time.Duration
	CleanupAfter    time.Duration
	CleanupInterval time.Duration // cleanup 실행 주기 (기본: 1시간)
	CleanupEnabled  bool
}

// DefaultDispatcherConfig: 기본 설정
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		BatchSize:       50,
		MaxConcurrent:   4,
		MaxRetries:      3,
		LockTimeout:     5 * time.Minute,
		PollInterval:    30 * time.Second,
		RetryBackoff:    1 * time.Minute,
		CleanupAfter:    7 * 24 * time.Hour,
		CleanupInterval: 1 * time.Hour,
		CleanupEnabled:  true,
	}
}

// Dispatcher: outbox 항목을 발송하는 goroutine worker
type Dispatcher struct {
	repo          deliveryRepository
	sender        MessageSender
	logger        *slog.Logger
	cfg           DispatcherConfig
	lastCleanupAt time.Time
}

// NewDispatcher: Dispatcher 생성
func NewDispatcher(repo deliveryRepository, sender MessageSender, logger *slog.Logger, cfg DispatcherConfig) *Dispatcher {
	defaults := DefaultDispatcherConfig()
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = defaults.MaxConcurrent
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.LockTimeout <= 0 {
		cfg.LockTimeout = defaults.LockTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults.PollInterval
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = defaults.RetryBackoff
	}
	if cfg.CleanupAfter <= 0 {
		cfg.CleanupAfter = defaults.CleanupAfter
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = defaults.CleanupInterval
	}
	return &Dispatcher{repo: repo, sender: sender, logger: logger, cfg: cfg}
}

// Start: ctx cancel로만 종료. 별도 Stop() 없음.
func (d *Dispatcher) Start(ctx context.Context) {
	go d.run(ctx)
}

func (d *Dispatcher) run(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	// 즉시 1회 실행
	d.processOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("Delivery dispatcher stopped")
			return
		case <-ticker.C:
			d.processOnce(ctx)
		}
	}
}

func (d *Dispatcher) processOnce(ctx context.Context) {
	items, err := d.repo.FetchAndLock(ctx, d.cfg.BatchSize, d.cfg.LockTimeout)
	if err != nil {
		d.logger.Error("Failed to fetch outbox items", slog.String("error", err.Error()))
		return
	}

	if len(items) == 0 {
		return
	}

	d.processBatch(ctx, items)

	// 모니터링: 실패 누적 경고
	if cnt, _ := d.repo.CountByStatus(ctx, domain.DeliveryStatusFailed); cnt > 5 {
		d.logger.Error("delivery outbox accumulated failures", slog.Int64("count", cnt))
	}

	// Cleanup (CleanupInterval 주기로만 실행)
	if d.cfg.CleanupEnabled && time.Since(d.lastCleanupAt) >= d.cfg.CleanupInterval {
		if cleaned, cleanErr := d.repo.Cleanup(ctx, d.cfg.CleanupAfter); cleanErr != nil {
			d.logger.Warn("Outbox cleanup failed", slog.String("error", cleanErr.Error()))
		} else if cleaned > 0 {
			d.logger.Info("Outbox cleanup completed", slog.Int64("removed", cleaned))
		}
		d.lastCleanupAt = time.Now()
	}
}

func (d *Dispatcher) processBatch(ctx context.Context, items []domain.NotificationDeliveryOutbox) {
	if len(items) == 0 {
		return
	}

	maxConcurrent := d.cfg.MaxConcurrent
	if maxConcurrent <= 1 || len(items) == 1 {
		for i := range items {
			d.processItem(ctx, &items[i])
		}
		return
	}
	if maxConcurrent > len(items) {
		maxConcurrent = len(items)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrent)

	for i := range items {
		select {
		case <-ctx.Done():
			d.logger.Warn("Delivery batch canceled before completion",
				slog.String("error", ctx.Err().Error()))
			wg.Wait()
			return
		case sem <- struct{}{}:
		}

		item := &items[i]
		wg.Add(1)
		go func(item *domain.NotificationDeliveryOutbox) {
			defer wg.Done()
			defer func() { <-sem }()
			d.processItem(ctx, item)
		}(item)
	}

	wg.Wait()
}

func (d *Dispatcher) processItem(ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	var p outboxPayload
	if err := json.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.logger.Error("Failed to unmarshal outbox payload",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
		_ = d.repo.MarkFailed(ctx, item.ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, "payload unmarshal: "+err.Error())
		return
	}

	if err := d.sender.SendMessage(ctx, item.RoomID, p.Message); err != nil {
		d.logger.Error("Failed to send outbox message",
			slog.Int64("id", item.ID),
			slog.String("room_id", item.RoomID),
			slog.String("error", err.Error()))
		_ = d.repo.MarkFailed(ctx, item.ID, d.cfg.MaxRetries, d.cfg.RetryBackoff, err.Error())
		return
	}

	if err := d.repo.MarkSent(ctx, item.ID); err != nil {
		d.logger.Error("Failed to mark outbox item as sent",
			slog.Int64("id", item.ID),
			slog.String("error", err.Error()))
	}
}

package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

// Start: 봇의 모든 구성 요소(스케줄러, 알람 체커, 관리자 서버)를 시작합니다.
func (r *BotRuntime) Start(ctx context.Context, errCh chan<- error) {
	if r == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r.startSchedulers(ctx, errCh)
	r.startBot(ctx)

	r.StartHTTPServer(errCh)
	if r.Logger != nil && r.ServerAddr != "" {
		r.Logger.Info("Bot HTTP server started", slog.String("addr", r.ServerAddr))
	}
}

func (r *BotRuntime) startSchedulers(ctx context.Context, errCh chan<- error) {
	if !r.IngestionEnabled {
		r.logInfo("Ingestion runtime disabled on bot process")
	} else if r.ingestionLease != nil {
		go r.ingestionLease.StartRenewLoop(ctx, errCh)
	}

	if r.IngestionEnabled && r.Scheduler != nil {
		r.Scheduler.Start(ctx)
		r.logInfo("YouTube ingestion scheduler started")
	}

	if r.IngestionEnabled && r.PhotoSync != nil {
		go r.PhotoSync.Start(ctx)
		r.logInfo("Photo sync service started (7-day interval)")
	}

	if r.IngestionEnabled && r.OutboxDispatcher != nil {
		r.OutboxDispatcher.Start(ctx)
		r.logInfo("YouTube outbox dispatcher started")
	}

	if r.IngestionEnabled && r.ScraperScheduler != nil {
		r.ScraperScheduler.Start(ctx)
		r.logInfo("Scraper scheduler started")
	}

	r.startAlarmScheduler(ctx)

	if r.ConfigSubscriber != nil {
		go r.ConfigSubscriber.Run(ctx)
		r.logInfo("Config subscriber started")
	}
}

func (r *BotRuntime) startAlarmScheduler(ctx context.Context) {
	if r.AlarmScheduler == nil {
		r.logInfo("Alarm runtime scheduler not configured")
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	alarmCtx, alarmCancel := context.WithCancel(ctx)
	r.setAlarmSchedulerCancel(alarmCancel)

	go r.AlarmScheduler.Start(alarmCtx)
	r.logInfo("Alarm runtime scheduler started")
}

func (r *BotRuntime) setAlarmSchedulerCancel(cancel context.CancelFunc) {
	if cancel == nil {
		return
	}

	r.alarmSchedulerMu.Lock()
	prevCancel := r.alarmSchedulerCancel
	r.alarmSchedulerCancel = cancel
	r.alarmSchedulerMu.Unlock()

	if prevCancel != nil {
		prevCancel()
	}
}

func (r *BotRuntime) clearAlarmSchedulerCancel() bool {
	r.alarmSchedulerMu.Lock()
	cancel := r.alarmSchedulerCancel
	r.alarmSchedulerCancel = nil
	r.alarmSchedulerMu.Unlock()

	if cancel != nil {
		cancel()
		return true
	}
	return false
}

func (r *BotRuntime) startBot(ctx context.Context) {
	if r.Bot == nil {
		return
	}
	go func() {
		if err := r.Bot.Start(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				r.logInfo("Bot alarm checker stopped (context done)")
			} else {
				r.logError("Bot alarm checker error", err)
			}
		}
	}()
}

func (r *BotRuntime) logInfo(msg string, attrs ...any) {
	if r.Logger != nil {
		r.Logger.Info(msg, attrs...)
	}
}

func (r *BotRuntime) logError(msg string, err error) {
	if r.Logger != nil {
		r.Logger.Error(msg, slog.Any("error", err))
	}
}

// Shutdown: 봇의 모든 구성 요소를 안전하게 종료하고 리소스를 정리합니다.
func (r *BotRuntime) Shutdown(ctx context.Context) {
	if r == nil {
		return
	}

	if r.clearAlarmSchedulerCancel() {
		r.logInfo("Alarm runtime scheduler cancellation signaled")
	}

	if r.Scheduler != nil {
		r.Scheduler.Stop()
		r.logInfo("YouTube ingestion scheduler stopped")
	}
	if r.ScraperScheduler != nil {
		r.ScraperScheduler.Stop()
		r.logInfo("Scraper scheduler stopped")
	}
	if err := r.ShutdownHTTPServer(ctx); err != nil {
		r.logError("HTTP server shutdown error", err)
	}
	if r.webhookHandlerCloser != nil {
		if err := r.webhookHandlerCloser.Close(); err != nil {
			r.logError("Iris webhook handler shutdown error", err)
		} else {
			r.logInfo("Iris webhook handler stopped")
		}
	}
	if err := notification.CloseAllAlarmServices(ctx); err != nil {
		r.logError("Alarm service shutdown error", err)
	} else {
		r.logInfo("Alarm services stopped")
	}
	if r.Bot != nil {
		if err := r.Bot.Shutdown(ctx); err != nil {
			r.logError("Error during shutdown", err)
		}
	}
	if r.ingestionLease != nil {
		if err := r.ingestionLease.Release(ctx); err != nil {
			r.logError("Ingestion lease release failed", err)
		}
	}
}

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

type runtimeAlarmScheduler interface {
	Start(ctx context.Context)
}

// BotRuntime: 봇 애플리케이션의 전체 실행 환경 및 상태를 관리하는 구조체
type BotRuntime struct {
	Config *config.Config
	Logger *slog.Logger

	Bot              *bot.Bot
	IngestionEnabled bool
	Scheduler        youtube.Scheduler
	ScraperScheduler *poller.Scheduler         // YouTube HTML 스크래퍼 기반 폴러 스케줄러
	PhotoSync        *holodex.PhotoSyncService // 프로필 이미지 동기화 서비스
	OutboxDispatcher *outbox.Dispatcher        // YouTube 알림 outbox 발송기
	AlarmScheduler   runtimeAlarmScheduler     // Alarm runtime scheduler

	ConfigSubscriber *configsub.Subscriber // Valkey Pub/Sub 설정 구독자

	ServerAddr string
	HttpServer *http.Server

	webhookHandlerCloser interface{ Close() error }
	ingestionLease       *providers.IngestionLease
	alarmSchedulerMu     sync.Mutex
	alarmSchedulerCancel context.CancelFunc
	cleanup              func()
}

// Close - 런타임 리소스 정리 (DB, 캐시 연결 해제)
func (r *BotRuntime) Close() {
	if r != nil && r.cleanup != nil {
		r.cleanup()
	}
}

// BuildRuntime: 설정과 로거를 기반으로 봇 런타임 환경을 구성하고 모든 의존성을 초기화합니다.
func BuildRuntime(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*BotRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runtime, cleanup, err := InitializeBotRuntime(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize runtime: %w", err)
	}
	runtime.cleanup = cleanup

	return runtime, nil
}

// StartHTTPServer: Bot HTTP 서버를 비동기적으로 시작합니다.
func (r *BotRuntime) StartHTTPServer(errCh chan<- error) {
	if r == nil || r.HttpServer == nil {
		return
	}

	go func() {
		if err := r.HttpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if errCh != nil {
				errCh <- fmt.Errorf("HTTP server error: %w", err)
				return
			}
			if r.Logger != nil {
				r.Logger.Error("HTTP server error", slog.Any("error", err))
			}
		}
	}()
}

// ShutdownHTTPServer: Bot HTTP 서버를 안전하게 종료합니다.
func (r *BotRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil || r.HttpServer == nil {
		return nil
	}
	if err := r.HttpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}
	return nil
}

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

// Run: 봇 애플리케이션을 실행하고 종료 신호(SIGINT, SIGTERM)를 대기한다. (블로킹)
func (r *BotRuntime) Run() {
	if r == nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	r.Start(ctx, errCh)
	if r.Logger != nil {
		r.Logger.Info("Bot started, waiting for signals...")
	}

	select {
	case sig := <-sigCh:
		if r.Logger != nil {
			r.Logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		}
	case err := <-errCh:
		if r.Logger != nil {
			r.Logger.Error("Server error", slog.Any("error", err))
		}
	}

	if r.Logger != nil {
		r.Logger.Info("Shutting down gracefully...")
	}
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Shutdown)
	defer shutdownCancel()

	r.Shutdown(shutdownCtx)

	if r.Logger != nil {
		r.Logger.Info("Shutdown complete")
	}
}

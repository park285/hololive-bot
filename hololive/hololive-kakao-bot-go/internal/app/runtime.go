package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
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

package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// Container: 애플리케이션의 모든 서비스와 의존성(Config, Logger, Services)을 관리하는 DI 컨테이너
type Container struct {
	Config *config.Config
	Logger *slog.Logger

	botDeps *bot.Dependencies
	cleanup func()
}

// Close - 컨테이너 리소스 정리 (DB, 캐시 연결 해제)
func (c *Container) Close() {
	if c != nil && c.cleanup != nil {
		c.cleanup()
	}
}

// NewBot: 설정된 의존성을 사용하여 새로운 Bot 인스턴스를 생성합니다.
func (c *Container) NewBot() (*bot.Bot, error) {
	if c.botDeps == nil {
		return nil, fmt.Errorf("bot dependencies not initialized")
	}
	b, err := bot.NewBot(c.botDeps)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot instance: %w", err)
	}
	return b, nil
}

// GetYouTubeScheduler: 유튜버 스케줄러 인스턴스를 반환합니다.
func (c *Container) GetYouTubeScheduler() *youtube.Scheduler {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Scheduler
}

// GetMemberRepo: 멤버 정보 저장소(Repository)를 반환합니다.
func (c *Container) GetMemberRepo() *member.Repository {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.MemberRepo
}

// GetMemberCache: 멤버 정보 캐시 서비스를 반환합니다.
func (c *Container) GetMemberCache() *member.Cache {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.MemberCache
}

// GetAlarmService: 알림 서비스를 반환합니다.
func (c *Container) GetAlarmService() domain.AlarmCRUD {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Alarm
}

// GetCache: 전역 캐시 서비스를 반환합니다.
func (c *Container) GetCache() *cache.Service {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Cache
}

// GetHolodexService: Holodex API 서비스를 반환합니다.
func (c *Container) GetHolodexService() domain.StreamProvider {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Holodex
}

// GetYouTubeService: YouTube API 서비스를 반환합니다.
func (c *Container) GetYouTubeService() *youtube.Service {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Service
}

// GetActivityLogger: 활동 로그 기록 서비스를 반환합니다.
func (c *Container) GetActivityLogger() *activity.Logger {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Activity
}

// GetSettingsService: 봇 설정 관리 서비스를 반환합니다.
func (c *Container) GetSettingsService() *settings.Service {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.Settings
}

// GetACLService: 접근 제어(ACL) 서비스를 반환합니다.
func (c *Container) GetACLService() *acl.Service {
	if c.botDeps == nil {
		return nil
	}
	return c.botDeps.ACL
}

// Build: 주어진 설정과 로거를 기반으로 애플리케이션 컨테이너를 구성하고 모든 의존성을 초기화합니다.
func Build(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Container, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 의존성 주입 초기화
	deps, cleanup, err := InitializeBotDependencies(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}

	return &Container{
		Config:  cfg,
		Logger:  logger,
		botDeps: deps,
		cleanup: cleanup,
	}, nil
}

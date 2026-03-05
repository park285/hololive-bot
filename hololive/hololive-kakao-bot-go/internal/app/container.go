package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
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

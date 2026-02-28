package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// StatsCommand: 구독자 순위 및 통계 조회 명령어를 처리하는 커맨드입니다.
type StatsCommand struct {
	deps *Dependencies
}

// NewStatsCommand: StatsCommand 인스턴스를 생성합니다.
func NewStatsCommand(deps *Dependencies) *StatsCommand {
	return &StatsCommand{deps: deps}
}

// Name: 커맨드 이름을 반환합니다.
func (c *StatsCommand) Name() string {
	return "stats"
}

// Description: 커맨드 설명을 반환합니다.
func (c *StatsCommand) Description() string {
	return "구독자 순위 및 통계 조회"
}

// Execute: 통계 커맨드를 실행합니다.
func (c *StatsCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(cmdCtx); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	action, _ := params["action"].(string)
	if action == "" {
		action = "gainers"
	}

	switch stringutil.Normalize(action) {
	case "gainers", "구독자순위":
		return c.showTopGainers(ctx, cmdCtx, params)
	default:
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.ErrUnknownStatsPeriod)
	}
}

func (c *StatsCommand) showTopGainers(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	periodStr, _ := params["period"].(string)
	now := time.Now()
	since, periodLabel := domain.ResolveStatsPeriod(now, periodStr)

	gainers, err := c.deps.StatsRepo.GetTopGainers(ctx, since, 10)
	if err != nil {
		c.deps.Logger.Error("Failed to get top gainers", slog.Any("error", err))
		return c.deps.SendError(ctx, cmdCtx.Room, adapter.ErrStatsQueryFailed)
	}

	if len(gainers) == 0 {
		return c.deps.SendMessage(ctx, cmdCtx.Room, adapter.MsgNoStatsData)
	}

	message := c.deps.Formatter.FormatStatsTopGainers(periodLabel, gainers)
	return c.deps.SendMessage(ctx, cmdCtx.Room, message)
}

func (c *StatsCommand) ensureDeps(cmdCtx *domain.CommandContext) error {
	if c == nil || c.deps == nil {
		return fmt.Errorf("stats command dependencies not configured")
	}

	if c.deps.SendMessage == nil || c.deps.SendError == nil {
		return fmt.Errorf("message callbacks not configured")
	}

	if c.deps.StatsRepo == nil {
		return fmt.Errorf("stats repository not configured")
	}

	if c.deps.Logger == nil {
		c.deps.Logger = slog.Default()
	}

	return nil
}

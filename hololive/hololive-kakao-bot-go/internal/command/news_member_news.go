package command

import (
	"context"
	stdErrors "errors"
	"fmt"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

// MemberNewsCommand: 구독 멤버 뉴스 조회 명령어.
type MemberNewsCommand struct {
	BaseCommand
}

// NewMemberNewsCommand: 명령어 생성.
func NewMemberNewsCommand(deps *Dependencies) *MemberNewsCommand {
	return &MemberNewsCommand{BaseCommand: NewBaseCommand(deps)}
}

func (c *MemberNewsCommand) Name() string {
	return "member_news"
}

func (c *MemberNewsCommand) Description() string {
	return "구독 멤버 뉴스 조회"
}

func (c *MemberNewsCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base deps: %w", err)
	}

	if c.Deps().MemberNews == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsServiceNotInitialized)
	}

	period := membernewscontracts.PeriodWeekly
	if rawPeriod, ok := params["period"].(string); ok {
		period = membernewscontracts.NormalizePeriod(membernewscontracts.Period(rawPeriod))
	}

	digest, err := c.Deps().MemberNews.GenerateRoomDigest(ctx, cmdCtx.Room, period)
	if err != nil {
		if stdErrors.Is(err, membernewscontracts.ErrNoSubscribedMembers) {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsNoMembers(ctx))
		}

		c.Deps().Logger.Error("Member news command failed", "room", cmdCtx.Room, "error", err)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrMemberNewsQueryFailed)
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsDigest(ctx, digest))
}

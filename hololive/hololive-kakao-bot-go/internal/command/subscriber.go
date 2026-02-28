package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// SubscriberCommand: 특정 멤버의 구독자 수를 조회하는 명령어
type SubscriberCommand struct {
	BaseCommand
}

// NewSubscriberCommand: 새로운 SubscriberCommand 인스턴스를 생성합니다.
func NewSubscriberCommand(deps *Dependencies) *SubscriberCommand {
	return &SubscriberCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 명령어 이름을 반환합니다.
func (c *SubscriberCommand) Name() string {
	return string(domain.CommandSubscriber)
}

// Description: 명령어 설명을 반환합니다.
func (c *SubscriberCommand) Description() string {
	return "특정 멤버의 구독자 수 조회"
}

// Execute: 명령어를 실행합니다.
func (c *SubscriberCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberQuery, _ := params["member"].(string)
	memberQuery = stringutil.TrimSpace(memberQuery)

	// 멤버 이름 필수
	if memberQuery == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrSubscriberNeedMemberName)
	}

	// 멤버 매칭
	matchedChannel, err := c.Deps().Matcher.FindBestMatch(ctx, memberQuery)
	if err != nil {
		c.log().Warn("Member match failed",
			slog.String("query", memberQuery),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberQuery))
	}
	if matchedChannel == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberQuery))
	}

	// Holodex API로 실시간 구독자 수 조회
	channel, err := c.Deps().Holodex.GetChannel(ctx, matchedChannel.ID)
	if err != nil {
		c.log().Error("Failed to get channel from Holodex",
			slog.String("channel_id", matchedChannel.ID),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrSubscriberQueryFailed)
	}

	if channel == nil || channel.SubscriberCount == nil || *channel.SubscriberCount == 0 {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.MsgNoSubscriberData)
	}

	// 멤버 정보 조회 (한글 이름 등)
	provider := c.Deps().MembersData.WithContext(ctx)
	member := provider.FindMemberByChannelID(channel.ID)

	memberName := channel.Name
	if member != nil && member.NameKo != "" {
		memberName = member.NameKo
	} else if channel.EnglishName != nil && *channel.EnglishName != "" {
		memberName = *channel.EnglishName
	}

	// 응답 메시지 생성
	message := c.Deps().Formatter.FormatSubscriberCount(memberName, uint64(*channel.SubscriberCount))
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *SubscriberCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil {
		return fmt.Errorf("matcher not configured")
	}
	if c.Deps().Holodex == nil {
		return fmt.Errorf("holodex service not configured")
	}
	if c.Deps().MembersData == nil {
		return fmt.Errorf("members data not configured")
	}
	if c.Deps().Formatter == nil {
		return fmt.Errorf("formatter not configured")
	}

	return nil
}

func (c *SubscriberCommand) log() *slog.Logger {
	if c.Deps() != nil && c.Deps().Logger != nil {
		return c.Deps().Logger
	}
	return slog.Default()
}

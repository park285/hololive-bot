package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

// MemberInfoCommand: 홀로라이브 멤버 프로필 조회 명령어를 처리하는 커맨드입니다.
type MemberInfoCommand struct {
	BaseCommand
}

// NewMemberInfoCommand: MemberInfoCommand 인스턴스를 생성합니다.
func NewMemberInfoCommand(deps *Dependencies) *MemberInfoCommand {
	return &MemberInfoCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 커맨드 이름을 반환합니다.
func (c *MemberInfoCommand) Name() string {
	return string(domain.CommandMemberInfo)
}

// Description: 커맨드 설명을 반환합니다.
func (c *MemberInfoCommand) Description() string {
	return "홀로라이브 멤버 공식 프로필"
}

// Execute: 멤버 정보 커맨드를 실행합니다.
// 쿼리가 없으면 멤버 디렉터리를, 있으면 개별 프로필을 표시합니다.
func (c *MemberInfoCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	rawQuery := getStringParam(params, "query")
	englishCandidate := getStringParam(params, "member")
	channelID := getStringParam(params, "channel_id")

	if stringutil.TrimSpace(rawQuery) == "" &&
		stringutil.TrimSpace(englishCandidate) == "" &&
		stringutil.TrimSpace(channelID) == "" {
		return c.renderMemberDirectory(ctx, cmdCtx)
	}

	member := c.resolveMember(ctx, channelID, englishCandidate, rawQuery)
	if member == nil {
		target := englishCandidate
		if target == "" {
			target = rawQuery
		}
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(target))
	}

	rawProfile, translated, err := c.Deps().OfficialProfiles.GetWithTranslation(ctx, member.Name)
	if err != nil {
		c.log().Error("Failed to load member profile",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, fmt.Sprintf(adapter.ErrMemberProfileLoadFailed, member.Name))
	}

	message := c.Deps().Formatter.FormatTalentProfile(rawProfile, translated)
	if message == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, fmt.Sprintf(adapter.ErrMemberProfileBuildFailed, member.Name))
	}

	if member.IsGraduated {
		message = adapter.MsgGraduatedMemberWarning + message
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *MemberInfoCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().MembersData == nil ||
		c.Deps().Formatter == nil || c.Deps().OfficialProfiles == nil {
		return fmt.Errorf("member info command services not configured")
	}

	return nil
}

func (c *MemberInfoCommand) resolveMember(ctx context.Context, channelID, englishName, query string) *domain.Member {
	provider := c.Deps().MembersData.WithContext(ctx)

	if channelID != "" {
		if member := provider.FindMemberByChannelID(channelID); member != nil {
			return member
		}
	}

	if englishName != "" {
		if member := provider.FindMemberByName(englishName); member != nil {
			return member
		}
	}

	trimmed := stringutil.TrimSpace(query)
	if trimmed == "" {
		return nil
	}

	channel, err := c.Deps().Matcher.FindBestMatch(ctx, trimmed)
	if err != nil {
		c.log().Warn("Member match failed",
			slog.String("query", trimmed),
			slog.Any("error", err),
		)
		return nil
	}
	if channel == nil {
		return nil
	}

	return provider.FindMemberByChannelID(channel.ID)
}

func (c *MemberInfoCommand) log() *slog.Logger {
	if c.Deps() != nil && c.Deps().Logger != nil {
		return c.Deps().Logger
	}
	return slog.Default()
}

func getStringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	val, ok := params[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return stringutil.TrimSpace(v)
	default:
		return stringutil.TrimSpace(fmt.Sprintf("%v", v))
	}
}

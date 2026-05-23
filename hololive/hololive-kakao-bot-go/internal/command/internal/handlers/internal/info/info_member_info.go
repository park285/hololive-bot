// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package info

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command/internal/handlers/internal/handlercore"
)

type MemberInfoCommand struct {
	handlercore.BaseCommand
}

func NewMemberInfoCommand(deps *handlercore.Dependencies) *MemberInfoCommand {
	return &MemberInfoCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *MemberInfoCommand) Name() string {
	return string(domain.CommandMemberInfo)
}

func (c *MemberInfoCommand) Description() string {
	return "홀로라이브 멤버 공식 프로필"
}

// 쿼리가 없으면 멤버 디렉터리를, 있으면 개별 프로필을 표시합니다.
func (c *MemberInfoCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	rawQuery := getStringParam(params, "query")
	englishCandidate := getStringParam(params, "member")
	channelID := getStringParam(params, "channel_id")

	if hasNoMemberInfoQuery(rawQuery, englishCandidate, channelID) {
		return c.renderMemberDirectory(ctx, cmdCtx)
	}

	member := c.resolveMember(ctx, channelID, englishCandidate, rawQuery)
	if member == nil {
		return c.sendMemberNotFound(ctx, cmdCtx.Room, englishCandidate, rawQuery)
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

func hasNoMemberInfoQuery(rawQuery, englishCandidate, channelID string) bool {
	return stringutil.TrimSpace(rawQuery) == "" &&
		stringutil.TrimSpace(englishCandidate) == "" &&
		stringutil.TrimSpace(channelID) == ""
}

func (c *MemberInfoCommand) sendMemberNotFound(ctx context.Context, room, englishCandidate, rawQuery string) error {
	target := englishCandidate
	if target == "" {
		target = rawQuery
	}

	return c.Deps().SendError(ctx, room, c.Deps().Formatter.MemberNotFound(target))
}

func (c *MemberInfoCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().MembersData == nil ||
		c.Deps().Formatter == nil || c.Deps().OfficialProfiles == nil {
		return errors.New("member info command services not configured")
	}

	return nil
}

func (c *MemberInfoCommand) resolveMember(ctx context.Context, channelID, englishName, query string) *domain.Member {
	provider := c.Deps().MembersData.WithContext(ctx)

	if member := findMemberByChannelID(provider, channelID); member != nil {
		return member
	}

	if member := findMemberByName(provider, englishName); member != nil {
		return member
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

func findMemberByChannelID(provider domain.MemberDataProvider, channelID string) *domain.Member {
	if channelID == "" {
		return nil
	}

	return provider.FindMemberByChannelID(channelID)
}

func findMemberByName(provider domain.MemberDataProvider, englishName string) *domain.Member {
	if englishName == "" {
		return nil
	}

	return provider.FindMemberByName(englishName)
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

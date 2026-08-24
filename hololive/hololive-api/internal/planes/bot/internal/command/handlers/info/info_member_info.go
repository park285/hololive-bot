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

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	"github.com/kapu/hololive-shared/pkg/domain"
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
		if err := c.renderMemberDirectory(ctx, cmdCtx); err != nil {
			return fmt.Errorf("render member directory: %w", err)
		}

		return nil
	}

	member, err := c.resolveRequestedMember(ctx, cmdCtx.Room, channelID, englishCandidate, rawQuery)
	if errors.Is(err, handlercore.ErrMemberLookupHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("resolve requested member: %w", err)
	}

	if member == nil {
		return nil
	}

	if err := c.sendMemberProfile(ctx, cmdCtx.Room, member); err != nil {
		return fmt.Errorf("send member profile: %w", err)
	}

	return nil
}

func (c *MemberInfoCommand) resolveRequestedMember(ctx context.Context, room, channelID, englishCandidate, rawQuery string) (*domain.Member, error) {
	member := c.resolveMember(ctx, channelID, englishCandidate, rawQuery)
	if member != nil {
		return member, nil
	}

	if err := c.sendMemberNotFound(ctx, room, englishCandidate, rawQuery); err != nil {
		return nil, fmt.Errorf("send member not found: %w", err)
	}

	return nil, handlercore.ErrMemberLookupHandled
}

func (c *MemberInfoCommand) sendMemberProfile(ctx context.Context, room string, member *domain.Member) error {
	rawProfile, translated, err := c.Deps().OfficialProfiles.GetWithTranslation(ctx, member.Name)
	if err != nil {
		c.log().Error("Failed to load member profile",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)

		if err := c.Deps().SendError(ctx, room, messaging.ErrMemberProfileLoadFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatTalentProfile(ctx, rawProfile, translated)
	if message == "" {
		if err := c.Deps().SendError(ctx, room, messaging.ErrMemberProfileBuildFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	if member.IsGraduated {
		message = c.Deps().Formatter.GraduatedMemberWarning(ctx) + message
	}

	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
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

	if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.MemberNotFound(ctx, target)); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
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

	channel, found, err := c.Deps().Matcher.FindBestMatch(ctx, trimmed)
	if err != nil {
		c.log().Warn("Member match failed",
			slog.String("query_token", privacylog.Pseudonym(trimmed)),
			slog.Any("error", err),
		)

		return nil
	}

	if !found {
		return nil
	}

	if channel == nil {
		c.log().Error("Member matcher returned found without channel")

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

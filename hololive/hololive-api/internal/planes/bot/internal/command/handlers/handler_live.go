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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type LiveCommand struct {
	handlercore.BaseCommand
}

func NewLiveCommand(deps *handlercore.Dependencies) *LiveCommand {
	return &LiveCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *LiveCommand) Name() string {
	return "live"
}

func (c *LiveCommand) Description() string {
	return "현재 방송 중인 스트림 목록"
}

// 특정 멤버 이름이 파라미터로 주어진 경우, 해당 멤버의 방송만 필터링한다.
func (c *LiveCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberName, hasMember := params[paramMember].(string)
	if hasMember && memberName != "" {
		if err := c.executeMemberLive(ctx, cmdCtx, memberName); err != nil {
			return fmt.Errorf("execute member live: %w", err)
		}

		return nil
	}

	if err := c.executeAllLive(ctx, cmdCtx); err != nil {
		return fmt.Errorf("execute all live: %w", err)
	}

	return nil
}

func (c *LiveCommand) executeMemberLive(ctx context.Context, cmdCtx *domain.CommandContext, memberName string) error {
	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "라이브")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to find member: %w", err)
	}

	if channel == nil {
		return nil
	}

	if err := c.sendMemberLiveStreams(ctx, cmdCtx.Room, channel); err != nil {
		return fmt.Errorf("send member live streams: %w", err)
	}

	return nil
}

func (c *LiveCommand) sendMemberLiveStreams(ctx context.Context, room string, channel *domain.Channel) error {
	return c.sendLiveQuery(ctx, room, livequery.Request{Scope: livequery.Member, ChannelID: channel.ID, MemberName: channel.Name, Limit: livequery.MaxItems})
}

func (c *LiveCommand) executeAllLive(ctx context.Context, cmdCtx *domain.CommandContext) error {
	return c.sendLiveQuery(ctx, cmdCtx.Room, livequery.Request{Scope: livequery.All, Limit: livequery.MaxItems})
}

func (c *LiveCommand) sendLiveQuery(ctx context.Context, room string, request livequery.Request) error {
	result, err := c.Deps().LiveQuery.Query(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("live query canceled: %w", ctx.Err())
		}

		if sendErr := c.Deps().SendError(ctx, room, messaging.ErrLiveStreamQueryFailed); sendErr != nil {
			return fmt.Errorf("send live query error: %w", sendErr)
		}

		return nil
	}

	if result.Status != livequery.Complete {
		// 사용자 응답에서 뺀 조회 범위 진단은 운영자가 부분·미확인 결과를 추적하도록 로그로 남긴다.
		c.Deps().Logger.InfoContext(ctx, "live query incomplete",
			slog.String("status", string(result.Status)),
			slog.Any("reasons", result.ReasonCounts()),
			slog.Int("items", len(result.Items)),
			slog.Time("as_of", result.AsOf),
		)
	}

	if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.LiveQuery(ctx, result, request.MemberName)); err != nil {
		return fmt.Errorf("send live query result: %w", err)
	}

	return nil
}

func (c *LiveCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().LiveQuery == nil || c.Deps().Formatter == nil {
		return errors.New("live command services not configured")
	}

	return nil
}

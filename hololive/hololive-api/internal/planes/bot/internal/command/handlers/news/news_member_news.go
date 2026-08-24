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

package news

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type MemberNewsCommand struct {
	handlercore.BaseCommand
}

func NewMemberNewsCommand(deps *handlercore.Dependencies) *MemberNewsCommand {
	return &MemberNewsCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
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
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrMemberNewsServiceNotInitialized); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	period := memberNewsPeriod(params)

	digest, err := c.Deps().MemberNews.GenerateRoomDigest(ctx, cmdCtx.Room, period)
	if err != nil {
		if replyErr := c.replyMemberNewsFailure(ctx, cmdCtx.Room, err); replyErr != nil {
			return fmt.Errorf("reply member news failure: %w", replyErr)
		}

		return nil
	}

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNewsDigest(ctx, digest)); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func memberNewsPeriod(params map[string]any) membernewscontracts.Period {
	rawPeriod, ok := params["period"].(string)
	if !ok {
		return membernewscontracts.PeriodWeekly
	}

	return membernewscontracts.NormalizePeriod(membernewscontracts.Period(rawPeriod))
}

func (c *MemberNewsCommand) replyMemberNewsFailure(ctx context.Context, room string, digestErr error) error {
	if stdErrors.Is(digestErr, membernewscontracts.ErrNoSubscribedMembers) {
		if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.FormatMemberNewsNoMembers(ctx)); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	c.Deps().Logger.Error("Member news command failed",
		privacylog.RoomIDAttr(room),
		slog.Any("error", digestErr),
	)

	if err := c.Deps().SendError(ctx, room, messaging.ErrMemberNewsQueryFailed); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}

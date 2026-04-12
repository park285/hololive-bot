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

package command

import (
	"context"
	stdErrors "errors"
	"fmt"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

type MemberNewsCommand struct {
	BaseCommand
}

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

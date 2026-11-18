package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
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

func (c *LiveCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
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

func (c *LiveCommand) executeMemberLive(ctx context.Context, cmdCtx *botcommand.CommandContext, memberName string) error {
	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "라이브")
	if memberLookupHandled(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
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

func (c *LiveCommand) executeAllLive(ctx context.Context, cmdCtx *botcommand.CommandContext) error {
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

	diagnostics := result.DiagnosticCounts()
	if result.Status != livequery.Complete || diagnostics != (livequery.Diagnostics{}) {
		reasons := result.ReasonCounts()
		c.Deps().Logger.InfoContext(ctx, "live query incomplete",
			slog.String("status", string(result.Status)),
			slog.Group("reasons",
				slog.Int("covered", reasons[livequery.Covered]),
				slog.Int("invalid_target", reasons[livequery.InvalidTarget]),
				slog.Int("invalid_projection", reasons[livequery.InvalidProjection]),
				slog.Int("uncollected", reasons[livequery.Uncollected]),
				slog.Int("incomplete", reasons[livequery.Incomplete]),
				slog.Int("stale", reasons[livequery.Stale]),
				slog.Int("invalid_clock", reasons[livequery.InvalidClock]),
				slog.Int("inconsistent", reasons[livequery.Inconsistent]),
				slog.Int("confirming_end", reasons[livequery.ConfirmingEnd])),
			slog.Group("nonblocking_diagnostics",
				slog.Int("retained_orphan_ends", diagnostics.RetainedOrphanEnds),
				slog.Int("ended_pending_ends", diagnostics.EndedPendingEnds),
				slog.Int("ended_head_mismatches", diagnostics.EndedHeadMismatches)),
			slog.Int("items", len(result.Items)),
			slog.Time("as_of", result.AsOf),
		)
	}

	videos := make([]collabmembers.Video, len(result.Items))
	for i, item := range result.Items {
		videos[i] = collabmembers.Video{ID: item.VideoID, HostChannelID: item.ChannelID}
	}

	collab, err := c.Deps().CollabMembers.DisplayNames(ctx, videos)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("collab members query canceled: %w", ctx.Err())
		}

		c.Deps().Logger.ErrorContext(ctx, "Failed to resolve live collab members", slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, room, messaging.ErrLiveStreamQueryFailed); sendErr != nil {
			return fmt.Errorf("send collab members error: %w", sendErr)
		}

		return nil
	}

	if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.LiveQuery(ctx, result, request.MemberName, collab)); err != nil {
		return fmt.Errorf("send live query result: %w", err)
	}

	return nil
}

func (c *LiveCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().LiveQuery == nil || c.Deps().Formatter == nil || c.Deps().CollabMembers == nil {
		return errors.New("live command services not configured")
	}

	return nil
}

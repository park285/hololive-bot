package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	broadcasttype "github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

type BroadcastHistoryCommand struct {
	handlercore.BaseCommand
}

type BroadcastThumbnailCommand struct {
	handlercore.BaseCommand
}

const (
	defaultBroadcastHistoryDays = 7
	MaxBroadcastHistoryDays     = 365
)

var errBroadcastHistoryHandled = errors.New("broadcast history request handled")

func NewBroadcastHistoryCommand(deps *handlercore.Dependencies) *BroadcastHistoryCommand {
	return &BroadcastHistoryCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func NewBroadcastThumbnailCommand(deps *handlercore.Dependencies) *BroadcastThumbnailCommand {
	return &BroadcastThumbnailCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *BroadcastHistoryCommand) Name() string {
	return "broadcast_history"
}

func (c *BroadcastHistoryCommand) Description() string {
	return "종료된 방송 이력 조회"
}

func (c *BroadcastHistoryCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	query, filter, err := c.buildQuery(ctx, cmdCtx, params)
	if errors.Is(err, errBroadcastHistoryHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	if query == nil {
		return nil
	}

	result, err := c.Deps().BroadcastHistory.ListEndedBroadcasts(ctx, query)
	if err != nil {
		c.Deps().Logger.Error("broadcast history query failed", slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastHistoryQueryFailed); sendErr != nil {
			return fmt.Errorf("send message: %w", sendErr)
		}

		return nil
	}

	filter.Truncated = result.Truncated

	message := c.Deps().Formatter.BroadcastHistory(ctx, *filter, broadcastHistoryFormatterEntries(result.Entries))

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *BroadcastHistoryCommand) buildQuery(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) (*handlercore.BroadcastHistoryQuery, *formatter.BroadcastHistoryFilter, error) {
	query, filter := newBroadcastHistoryQuery(params)
	if err := c.applyBroadcastHistoryType(ctx, cmdCtx, params, &query, &filter); err != nil {
		return nil, nil, fmt.Errorf("apply broadcast history type: %w", err)
	}

	if err := c.applyBroadcastHistoryMember(ctx, cmdCtx, params, &query, &filter); err != nil {
		return nil, nil, fmt.Errorf("apply broadcast history member: %w", err)
	}

	return &query, &filter, nil
}

func newBroadcastHistoryQuery(params map[string]any) (query handlercore.BroadcastHistoryQuery, filter formatter.BroadcastHistoryFilter) {
	days := normalizeBroadcastHistoryDays(intParam(params, "days", defaultBroadcastHistoryDays))
	if boolParam(params, "all") {
		days = MaxBroadcastHistoryDays
	}

	limit := normalizeBroadcastHistoryLimit(intParam(params, "limit", defaultBroadcastHistoryLimit))

	query = handlercore.BroadcastHistoryQuery{
		Limit:   limit,
		TopicID: stringParam(params, "topic"),
		Since:   time.Now().AddDate(0, 0, -days),
	}

	return query, formatter.BroadcastHistoryFilter{
		TopicID: query.TopicID,
		Days:    days,
		Limit:   limit,
	}
}

func (c *BroadcastHistoryCommand) applyBroadcastHistoryType(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *formatter.BroadcastHistoryFilter) error {
	rawType := stringParam(params, "type")
	if rawType == "" {
		return nil
	}

	typ, ok := broadcasttype.Parse(rawType)
	if !ok {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastTypeUnknown); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return errBroadcastHistoryHandled
	}

	query.Type = string(typ)
	filter.TypeLabel = typ.Label()

	return nil
}

func (c *BroadcastHistoryCommand) applyBroadcastHistoryMember(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *formatter.BroadcastHistoryFilter) error {
	memberName := stringParam(params, paramMember)
	if memberName == "" {
		return nil
	}

	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "방송 이력")
	if memberLookupHandled(err) {
		return errBroadcastHistoryHandled
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	if channel == nil {
		return errBroadcastHistoryHandled
	}

	query.ChannelID = channel.ID
	filter.MemberName = channel.Name

	return nil
}

func (c *BroadcastHistoryCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().BroadcastHistory == nil || c.Deps().Formatter == nil || c.Deps().Matcher == nil {
		return errors.New("broadcast history services not configured")
	}

	return nil
}

func (c *BroadcastThumbnailCommand) Name() string {
	return "broadcast_thumbnail"
}

func (c *BroadcastThumbnailCommand) Description() string {
	return "종료된 방송 썸네일 다운로드"
}

func (c *BroadcastThumbnailCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	videoID := stringParam(params, "video_id")
	if videoID == "" {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastThumbnailUsage); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	if err := c.sendBroadcastThumbnail(ctx, cmdCtx, videoID); err != nil {
		return fmt.Errorf("send broadcast thumbnail: %w", err)
	}

	return nil
}

func (c *BroadcastThumbnailCommand) sendBroadcastThumbnail(ctx context.Context, cmdCtx *botcommand.CommandContext, videoID string) error {
	entry, err := c.lookupBroadcastThumbnailEntry(ctx, cmdCtx, videoID)
	if errors.Is(err, errBroadcastHistoryHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("lookup broadcast thumbnail entry: %w", err)
	}

	image, contentType, err := c.downloadBroadcastThumbnail(ctx, cmdCtx, entry)
	if errors.Is(err, errBroadcastHistoryHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("download broadcast thumbnail: %w", err)
	}

	if err := c.Deps().SendImage(transport.WithImageContentType(ctx, contentType), cmdCtx.Room, image); err != nil {
		return fmt.Errorf("send image: %w", err)
	}

	return nil
}

func (c *BroadcastThumbnailCommand) lookupBroadcastThumbnailEntry(ctx context.Context, cmdCtx *botcommand.CommandContext, videoID string) (*handlercore.BroadcastHistoryEntry, error) {
	entry, err := c.Deps().BroadcastHistory.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: videoID})
	if err != nil && !errors.Is(err, handlercore.ErrBroadcastNotFound) {
		c.Deps().Logger.Error("broadcast thumbnail lookup failed", slog.String("video_id", videoID), slog.Any("error", err))

		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastHistoryQueryFailed); sendErr != nil {
			return nil, fmt.Errorf("send message: %w", sendErr)
		}

		return nil, errBroadcastHistoryHandled
	}

	if entry == nil {
		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastThumbnailNotFound); sendErr != nil {
			return nil, fmt.Errorf("send message: %w", sendErr)
		}

		return nil, errBroadcastHistoryHandled
	}

	return entry, nil
}

func (c *BroadcastThumbnailCommand) downloadBroadcastThumbnail(ctx context.Context, cmdCtx *botcommand.CommandContext, entry *handlercore.BroadcastHistoryEntry) ([]byte, string, error) {
	if entry == nil {
		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastThumbnailNotFound); sendErr != nil {
			return nil, "", fmt.Errorf("send message: %w", sendErr)
		}

		return nil, "", errBroadcastHistoryHandled
	}

	image, contentType, err := c.Deps().ThumbnailDownloader.Download(ctx, entry)
	if err == nil {
		return image, contentType, nil
	}

	c.Deps().Logger.Error("broadcast thumbnail download failed", slog.String("video_id", entry.VideoID), slog.Any("error", err))

	if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrBroadcastThumbnailDownloadFailed); sendErr != nil {
		return nil, "", fmt.Errorf("send message: %w", sendErr)
	}

	return nil, "", errBroadcastHistoryHandled
}

func (c *BroadcastThumbnailCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().BroadcastHistory == nil || c.Deps().ThumbnailDownloader == nil || c.Deps().SendImage == nil {
		return errors.New("broadcast thumbnail services not configured")
	}

	return nil
}

func broadcastHistoryFormatterEntries(entries []handlercore.BroadcastHistoryEntry) []formatter.BroadcastHistoryEntry {
	result := make([]formatter.BroadcastHistoryEntry, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		fallbackTime, fallbackLabel := broadcastHistoryFallbackTime(entry)
		endedAt := verifiedBroadcastEndedAt(entry)
		archiveUnavailable := entry.TimesSource != "provider" && endedAt.IsZero() &&
			entry.FinalizationOutcome != nil && *entry.FinalizationOutcome == "exhausted" &&
			entry.FinalizationReason != nil && *entry.FinalizationReason == "identity_missing"

		if archiveUnavailable && fallbackLabel == "종료" {
			fallbackLabel = "기록"
		}

		result = append(result, formatter.BroadcastHistoryEntry{
			VideoID:            entry.VideoID,
			MemberName:         mekparkhost.DisplayName(entry.ChannelID, entry.Title, entry.MemberName),
			Type:               entry.BroadcastType,
			TypeLabel:          broadcasttype.Type(entry.BroadcastType).Label(),
			TopicID:            entry.TopicID,
			Title:              entry.Title,
			StartedAt:          broadcastHistoryStartedAt(entry),
			EndedAt:            endedAt,
			Time:               fallbackTime,
			TimeLabel:          fallbackLabel,
			URL:                "https://www.youtube.com/watch?v=" + entry.VideoID,
			HasThumbnail:       validYouTubeVideoID(entry.VideoID),
			ArchiveUnavailable: archiveUnavailable,
		})
	}

	return result
}

const maxBroadcastEndUncertainty = 5 * time.Minute

const legacyLifecycleOrigin = "legacy_unknown"

func trustedBroadcastStartedAt(entry *handlercore.BroadcastHistoryEntry) *time.Time {
	if entry.TimesSource != "provider" && entry.LifecycleOrigin == legacyLifecycleOrigin {
		return nil
	}

	return entry.StartedAt
}

func broadcastHistoryStartedAt(entry *handlercore.BroadcastHistoryEntry) time.Time {
	startedAt := trustedBroadcastStartedAt(entry)
	if startedAt == nil {
		return time.Time{}
	}

	return *startedAt
}

func verifiedBroadcastEndedAt(entry *handlercore.BroadcastHistoryEntry) time.Time {
	startedAt := trustedBroadcastStartedAt(entry)
	if startedAt == nil || entry.EndedAt == nil {
		return time.Time{}
	}

	if entry.EndedAt.Before(*startedAt) {
		return time.Time{}
	}

	if entry.TimesSource == "provider" {
		return *entry.EndedAt
	}

	if entry.TimesSource != "observed" || entry.LifecycleOrigin != "observed" || entry.LastLivePositiveAt == nil {
		return time.Time{}
	}

	gap := entry.EndedAt.Sub(*entry.LastLivePositiveAt)
	if gap > maxBroadcastEndUncertainty || gap < -maxBroadcastEndUncertainty {
		return time.Time{}
	}

	return *entry.EndedAt
}

func broadcastHistoryFallbackTime(entry *handlercore.BroadcastHistoryEntry) (time.Time, string) {
	for _, candidate := range []struct {
		at    *time.Time
		label string
	}{
		{entry.EndedAt, "종료"},
		{entry.ScheduledStartTime, "예정"},
	} {
		if candidate.at != nil && !candidate.at.IsZero() {
			return *candidate.at, candidate.label
		}
	}

	return entry.LastSeenAt, "마지막 확인"
}

func normalizeBroadcastHistoryDays(days int) int {
	if days <= 0 {
		return defaultBroadcastHistoryDays
	}

	if days > MaxBroadcastHistoryDays {
		return MaxBroadcastHistoryDays
	}

	return days
}

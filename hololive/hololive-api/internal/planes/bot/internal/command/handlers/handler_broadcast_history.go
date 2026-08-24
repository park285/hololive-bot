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
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	broadcasttype "github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type BroadcastHistoryCommand struct {
	handlercore.BaseCommand
}

type BroadcastThumbnailCommand struct {
	handlercore.BaseCommand
}

const (
	defaultBroadcastHistoryDays = 7
	maxBroadcastHistoryDays     = 365
)

const broadcastThumbnailNotFoundMessage = "종료된 방송 이력에서 해당 video_id를 찾지 못했습니다."

// 사용자-facing 응답을 이미 보냈으니 호출자는 추가 응답 없이 종료해야 한다.
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

func (c *BroadcastHistoryCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

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

		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "방송 이력 조회 중 오류가 발생했습니다."); sendErr != nil {
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

func (c *BroadcastHistoryCommand) buildQuery(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) (*handlercore.BroadcastHistoryQuery, *formatter.BroadcastHistoryFilter, error) {
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
	days := normalizeBroadcastHistoryDays(intBroadcastHistoryParam(params, "days", defaultBroadcastHistoryDays))
	if boolParam(params, "all") {
		days = maxBroadcastHistoryDays
	}

	limit := normalizeBroadcastHistoryLimit(intBroadcastHistoryParam(params, "limit", defaultBroadcastHistoryLimit))

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

func (c *BroadcastHistoryCommand) applyBroadcastHistoryType(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *formatter.BroadcastHistoryFilter) error {
	rawType := stringParam(params, "type")
	if rawType == "" {
		return nil
	}

	typ, ok := broadcasttype.Parse(rawType)
	if !ok {
		if err := c.Deps().SendMessage(ctx, cmdCtx.Room, "알 수 없는 방송 타입입니다. 사용 가능: 게임, 잡담, 노래, ASMR, 멤버십, 이벤트, 경마, 동시시청, 뉴스, 기타, 미분류"); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return errBroadcastHistoryHandled
	}

	query.Type = string(typ)
	filter.TypeLabel = typ.Label()

	return nil
}

func (c *BroadcastHistoryCommand) applyBroadcastHistoryMember(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *formatter.BroadcastHistoryFilter) error {
	memberName := stringParam(params, paramMember)
	if memberName == "" {
		return nil
	}

	if c.Deps().Matcher == nil {
		return errors.New("broadcast history matcher not configured")
	}

	channel, err := handlercore.FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "방송 이력")
	if memberLookupHandled(err) {
		return errBroadcastHistoryHandled
	}

	if err != nil {
		return fmt.Errorf("failed to find member: %w", err)
	}

	if channel == nil {
		return errBroadcastHistoryHandled
	}

	query.ChannelID = channel.ID
	filter.MemberName = channel.Name

	return nil
}

func (c *BroadcastHistoryCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().BroadcastHistory == nil || c.Deps().Formatter == nil {
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

func (c *BroadcastThumbnailCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	videoID := stringParam(params, "video_id")
	if videoID == "" {
		if err := c.Deps().SendMessage(ctx, cmdCtx.Room, "사용법: 방송이력 썸네일 <video_id>"); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	if err := c.sendBroadcastThumbnail(ctx, cmdCtx, videoID); err != nil {
		return fmt.Errorf("send broadcast thumbnail: %w", err)
	}

	return nil
}

func (c *BroadcastThumbnailCommand) sendBroadcastThumbnail(ctx context.Context, cmdCtx *domain.CommandContext, videoID string) error {
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

func (c *BroadcastThumbnailCommand) lookupBroadcastThumbnailEntry(ctx context.Context, cmdCtx *domain.CommandContext, videoID string) (*handlercore.BroadcastHistoryEntry, error) {
	entry, err := c.Deps().BroadcastHistory.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: videoID})
	if err != nil && !errors.Is(err, handlercore.ErrBroadcastNotFound) {
		c.Deps().Logger.Error("broadcast thumbnail lookup failed", slog.String("video_id", videoID), slog.Any("error", err))

		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "방송 이력 조회 중 오류가 발생했습니다."); sendErr != nil {
			return nil, fmt.Errorf("send message: %w", sendErr)
		}

		return nil, errBroadcastHistoryHandled
	}

	if entry == nil {
		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, broadcastThumbnailNotFoundMessage); sendErr != nil {
			return nil, fmt.Errorf("send message: %w", sendErr)
		}

		return nil, errBroadcastHistoryHandled
	}

	return entry, nil
}

func (c *BroadcastThumbnailCommand) downloadBroadcastThumbnail(ctx context.Context, cmdCtx *domain.CommandContext, entry *handlercore.BroadcastHistoryEntry) ([]byte, string, error) {
	if entry == nil {
		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, broadcastThumbnailNotFoundMessage); sendErr != nil {
			return nil, "", fmt.Errorf("send message: %w", sendErr)
		}

		return nil, "", errBroadcastHistoryHandled
	}

	image, contentType, err := c.Deps().ThumbnailDownloader.Download(ctx, entry)
	if err == nil {
		return image, contentType, nil
	}

	c.Deps().Logger.Error("broadcast thumbnail download failed", slog.String("video_id", entry.VideoID), slog.Any("error", err))

	if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "고화질 썸네일을 다운로드하지 못했습니다."); sendErr != nil {
		return nil, "", fmt.Errorf("send message: %w", sendErr)
	}

	return nil, "", errBroadcastHistoryHandled
}

func (c *BroadcastThumbnailCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
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

		result = append(result, formatter.BroadcastHistoryEntry{
			VideoID:      entry.VideoID,
			MemberName:   entry.MemberName,
			Type:         entry.BroadcastType,
			TypeLabel:    broadcasttype.Type(entry.BroadcastType).Label(),
			TopicID:      entry.TopicID,
			Title:        entry.Title,
			Time:         broadcastHistoryEntryTime(entry),
			URL:          "https://www.youtube.com/watch?v=" + entry.VideoID,
			HasThumbnail: validYouTubeVideoID(entry.VideoID),
		})
	}

	return result
}

func broadcastHistoryEntryTime(entry *handlercore.BroadcastHistoryEntry) time.Time {
	for _, candidate := range []*time.Time{entry.EndedAt, entry.StartedAt, entry.ScheduledStartTime} {
		if candidate != nil && !candidate.IsZero() {
			return *candidate
		}
	}

	return entry.LastSeenAt
}

func intBroadcastHistoryParam(params map[string]any, key string, defaultValue int) int {
	raw, ok := params[key]
	if !ok {
		return defaultValue
	}

	switch value := raw.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return defaultValue
	}
}

func normalizeBroadcastHistoryDays(days int) int {
	if days <= 0 {
		return defaultBroadcastHistoryDays
	}

	if days > maxBroadcastHistoryDays {
		return maxBroadcastHistoryDays
	}

	return days
}

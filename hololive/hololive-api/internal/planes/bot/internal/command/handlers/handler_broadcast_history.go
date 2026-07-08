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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type BroadcastHistoryCommand struct {
	BaseCommand
}

type BroadcastThumbnailCommand struct {
	BaseCommand
}

const (
	defaultBroadcastHistoryDays = 7
	maxBroadcastHistoryDays     = 365
)

func NewBroadcastHistoryCommand(deps *Dependencies) *BroadcastHistoryCommand {
	return &BroadcastHistoryCommand{BaseCommand: NewBaseCommand(deps)}
}

func NewBroadcastThumbnailCommand(deps *Dependencies) *BroadcastThumbnailCommand {
	return &BroadcastThumbnailCommand{BaseCommand: NewBaseCommand(deps)}
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
	if err != nil {
		return err
	}
	if query == nil {
		return nil
	}

	entries, err := c.Deps().BroadcastHistory.ListEndedBroadcasts(ctx, query)
	if err != nil {
		c.Deps().Logger.Error("broadcast history query failed", slog.Any("error", err))
		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "방송 이력 조회 중 오류가 발생했습니다."); sendErr != nil {
			return sendErr
		}
		return nil
	}

	message := c.Deps().Formatter.BroadcastHistory(ctx, *filter, broadcastHistoryFormatterEntries(entries))
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *BroadcastHistoryCommand) buildQuery(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) (*handlercore.BroadcastHistoryQuery, *adapter.BroadcastHistoryFilter, error) {
	query, filter := newBroadcastHistoryQuery(params)
	if handled, err := c.applyBroadcastHistoryType(ctx, cmdCtx, params, &query, &filter); handled || err != nil {
		return nil, nil, err
	}
	if handled, err := c.applyBroadcastHistoryMember(ctx, cmdCtx, params, &query, &filter); handled || err != nil {
		return nil, nil, err
	}
	return &query, &filter, nil
}

func newBroadcastHistoryQuery(params map[string]any) (query handlercore.BroadcastHistoryQuery, filter adapter.BroadcastHistoryFilter) {
	days := normalizeBroadcastHistoryDays(intBroadcastHistoryParam(params, "days", defaultBroadcastHistoryDays))
	limit := normalizeBroadcastHistoryLimit(intBroadcastHistoryParam(params, "limit", defaultBroadcastHistoryLimit))
	query = handlercore.BroadcastHistoryQuery{
		Limit:      limit,
		TopicID:    stringParam(params, "topic"),
		IncludeAll: boolParam(params, "all"),
	}
	if !query.IncludeAll {
		query.Since = time.Now().AddDate(0, 0, -days)
	}
	return query, adapter.BroadcastHistoryFilter{
		TopicID:    query.TopicID,
		Days:       days,
		Limit:      limit,
		IncludeAll: query.IncludeAll,
	}
}

func (c *BroadcastHistoryCommand) applyBroadcastHistoryType(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *adapter.BroadcastHistoryFilter) (bool, error) {
	rawType := stringParam(params, "type")
	if rawType == "" {
		return false, nil
	}
	typ, ok := ParseBroadcastType(rawType)
	if !ok {
		return true, c.Deps().SendMessage(ctx, cmdCtx.Room, "알 수 없는 방송 타입입니다. 사용 가능: 게임, 잡담, 노래, ASMR, 멤버십, 이벤트, 경마, 동시시청, 뉴스, 기타, 미분류")
	}
	query.Type = string(typ)
	filter.TypeLabel = typ.Label()
	return false, nil
}

func (c *BroadcastHistoryCommand) applyBroadcastHistoryMember(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any, query *handlercore.BroadcastHistoryQuery, filter *adapter.BroadcastHistoryFilter) (bool, error) {
	memberName := stringParam(params, "member")
	if memberName == "" {
		return false, nil
	}
	if c.Deps().Matcher == nil {
		return false, errors.New("broadcast history matcher not configured")
	}

	channel, err := FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName, "방송 이력")
	if memberLookupHandled(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to find member %q: %w", memberName, err)
	}
	if channel == nil {
		return true, nil
	}

	query.ChannelID = channel.ID
	filter.MemberName = channel.Name
	return false, nil
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
		return c.Deps().SendMessage(ctx, cmdCtx.Room, "사용법: 방송이력 썸네일 <video_id>")
	}

	entry, handled, err := c.lookupBroadcastThumbnailEntry(ctx, cmdCtx, videoID)
	if handled || err != nil {
		return err
	}
	image, contentType, handled, err := c.downloadBroadcastThumbnail(ctx, cmdCtx, entry)
	if handled || err != nil {
		return err
	}

	return c.Deps().SendImage(ctx, cmdCtx.Room, image, iris.WithImageContentType(contentType))
}

func (c *BroadcastThumbnailCommand) lookupBroadcastThumbnailEntry(ctx context.Context, cmdCtx *domain.CommandContext, videoID string) (*handlercore.BroadcastHistoryEntry, bool, error) {
	entry, err := c.Deps().BroadcastHistory.GetEndedBroadcast(ctx, handlercore.BroadcastThumbnailQuery{VideoID: videoID})
	if err != nil {
		c.Deps().Logger.Error("broadcast thumbnail lookup failed", slog.String("video_id", videoID), slog.Any("error", err))
		if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "방송 이력 조회 중 오류가 발생했습니다."); sendErr != nil {
			return nil, true, sendErr
		}
		return nil, true, nil
	}
	if entry == nil {
		return nil, true, c.Deps().SendMessage(ctx, cmdCtx.Room, "종료된 방송 이력에서 해당 video_id를 찾지 못했습니다.")
	}
	return entry, false, nil
}

func (c *BroadcastThumbnailCommand) downloadBroadcastThumbnail(ctx context.Context, cmdCtx *domain.CommandContext, entry *handlercore.BroadcastHistoryEntry) (image []byte, contentType string, handled bool, err error) {
	if entry == nil {
		return nil, "", true, c.Deps().SendMessage(ctx, cmdCtx.Room, "종료된 방송 이력에서 해당 video_id를 찾지 못했습니다.")
	}
	image, contentType, err = c.Deps().ThumbnailDownloader.Download(ctx, entry)
	if err == nil {
		return image, contentType, false, nil
	}
	c.Deps().Logger.Error("broadcast thumbnail download failed", slog.String("video_id", entry.VideoID), slog.Any("error", err))
	if sendErr := c.Deps().SendMessage(ctx, cmdCtx.Room, "고화질 썸네일을 다운로드하지 못했습니다."); sendErr != nil {
		return nil, "", true, sendErr
	}
	return nil, "", true, nil
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

func broadcastHistoryFormatterEntries(entries []handlercore.BroadcastHistoryEntry) []adapter.BroadcastHistoryEntry {
	result := make([]adapter.BroadcastHistoryEntry, 0, len(entries))
	for i := range entries {
		entry := &entries[i]
		result = append(result, adapter.BroadcastHistoryEntry{
			VideoID:      entry.VideoID,
			MemberName:   entry.MemberName,
			Type:         entry.BroadcastType,
			TypeLabel:    BroadcastType(entry.BroadcastType).Label(),
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

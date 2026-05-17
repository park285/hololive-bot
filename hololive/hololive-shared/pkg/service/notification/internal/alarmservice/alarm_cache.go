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

package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/valkey-io/valkey-go"
)

func (as *AlarmService) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	if err := as.cache.HSet(ctx, MemberNameKey, channelID, memberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	return nil
}

func (as *AlarmService) GetMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := as.cache.HGet(ctx, MemberNameKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}

	return name, nil
}

func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	if name := as.resolveMemberDataName(ctx, channelID); name != "" {
		return name
	}
	return stringutil.TrimSpace(fallback)
}

func (as *AlarmService) resolveMemberDataName(ctx context.Context, channelID string) string {
	provider := as.memberData
	if provider == nil {
		return ""
	}
	if scoped := provider.WithContext(ctx); scoped != nil {
		provider = scoped
	}
	member := provider.FindMemberByChannelID(channelID)
	if member == nil {
		return ""
	}
	return firstMemberName(member.ShortKoreanName, member.NameKo, member.Name)
}

func firstMemberName(candidates ...string) string {
	for _, candidate := range candidates {
		if name := stringutil.TrimSpace(candidate); name != "" {
			return name
		}
	}
	return ""
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	subscribers, err := sharedalarm.LookupChannelSubscribersByType(ctx, as.cache, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: %w", err)
	}

	return subscribers, nil
}

func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	if err := as.cache.HSet(ctx, RoomNamesCacheKey, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}

func (as *AlarmService) SetUserName(ctx context.Context, userID, userName string) error {
	if err := as.cache.HSet(ctx, UserNamesCacheKey, userID, userName); err != nil {
		return fmt.Errorf("set user name: %w", err)
	}

	return nil
}

func normalizeScheduledMinute(startScheduled time.Time) time.Time {
	return startScheduled.Truncate(time.Minute)
}

func buildTitleFingerprint(title, streamID string) string {
	return keys.BuildTitleFingerprint(title, streamID)
}

func resolveStreamChannelID(stream *domain.Stream, defaultChannelID string) string {
	if stream == nil {
		return defaultChannelID
	}

	channelID := stringutil.TrimSpace(stream.ChannelID)
	if channelID != "" {
		return channelID
	}

	if stream.Channel != nil {
		channelID = stringutil.TrimSpace(stream.Channel.ID)
		if channelID != "" {
			return channelID
		}
	}

	return defaultChannelID
}

func (as *AlarmService) buildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	scheduledMinute := normalizeScheduledMinute(startScheduled).Unix()
	titleFingerprint := buildTitleFingerprint(title, streamID)

	return fmt.Sprintf(
		"%s%s:%s:%d:%s",
		UpcomingEventKeyPrefix,
		roomID,
		channelID,
		scheduledMinute,
		titleFingerprint,
	)
}

func (as *AlarmService) MarkUpcomingEventNotified(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
) error {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	resolvedChannelID := resolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return nil
	}

	key := as.buildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := as.cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		as.logger.Warn("Failed to mark upcoming event notified",
			slog.String("key", key),
			slog.String("room_id", roomID),
			slog.String("channel_id", resolvedChannelID),
			slog.String("stream_id", stream.ID),
			slog.Any("error", err),
		)

		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}

func (as *AlarmService) WasUpcomingEventNotifiedRecently(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
	window time.Duration,
) bool {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return false
	}

	resolvedChannelID := resolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return false
	}

	key := as.buildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data UpcomingEventNotifiedData
	if err := as.cache.Get(ctx, key, &data); err != nil || data.NotifiedAt == "" {
		return false
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false
	}

	if window <= 0 {
		return false
	}

	return time.Since(notifiedAt) <= window
}

func (as *AlarmService) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	key := NextStreamKeyPrefix + channelID

	data, err := as.cache.HGetAll(ctx, key)
	if err != nil {
		as.logger.Error("Failed to get next stream info from cache",
			slog.String("channel_id", channelID),
			slog.Any("error", err),
		)

		return nil, fmt.Errorf("get next stream info: %w", err)
	}

	return as.parseNextStreamInfo(channelID, data), nil
}

func (as *AlarmService) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("list_view", startedAt, opErr)
	}()

	alarms, err := as.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: %w", err)
		return nil, opErr
	}

	if len(alarms) == 0 {
		return []domain.AlarmListView{}, nil
	}

	channelIDs := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		channelIDs = append(channelIDs, alarm.ChannelID)
	}

	memberNames, err := as.getMemberNamesBatch(ctx, channelIDs)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: get member names batch: %w", err)
		return nil, opErr
	}

	nextStreams, err := as.getNextStreamInfosBatch(ctx, channelIDs)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: get next stream info batch: %w", err)
		return nil, opErr
	}

	return buildAlarmListViews(alarms, memberNames, nextStreams), nil
}

func buildAlarmListViews(
	alarms []*domain.Alarm,
	memberNames map[string]string,
	nextStreams map[string]*domain.NextStreamInfo,
) []domain.AlarmListView {
	entries := make([]domain.AlarmListView, 0, len(alarms))
	for _, alarm := range alarms {
		memberName := stringutil.TrimSpace(memberNames[alarm.ChannelID])
		if memberName == "" {
			memberName = stringutil.TrimSpace(alarm.MemberName)
		}

		if memberName == "" {
			memberName = alarm.ChannelID
		}

		entries = append(entries, domain.AlarmListView{
			ChannelID:  alarm.ChannelID,
			MemberName: memberName,
			AlarmTypes: alarm.AlarmTypes,
			NextStream: nextStreams[alarm.ChannelID],
		})
	}

	return entries
}

func (as *AlarmService) getMemberNamesBatch(ctx context.Context, channelIDs []string) (map[string]string, error) {
	if len(channelIDs) == 0 {
		return map[string]string{}, nil
	}

	return as.cache.BatchHGet(ctx, MemberNameKey, channelIDs)
}

func (as *AlarmService) getNextStreamInfosBatch(ctx context.Context, channelIDs []string) (map[string]*domain.NextStreamInfo, error) {
	if len(channelIDs) == 0 {
		return map[string]*domain.NextStreamInfo{}, nil
	}

	builder := as.cache.Builder()

	cmds := make([]valkey.Completed, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		cmds = append(cmds, builder.Hgetall().Key(NextStreamKeyPrefix+channelID).Build())
	}

	results := as.cache.DoMulti(ctx, cmds...)
	infos := make(map[string]*domain.NextStreamInfo, len(channelIDs))

	for i, result := range results {
		data, err := result.AsStrMap()
		if err != nil || len(data) == 0 {
			continue
		}

		if info := as.parseNextStreamInfo(channelIDs[i], data); info != nil {
			infos[channelIDs[i]] = info
		}
	}

	return infos, nil
}

func (as *AlarmService) parseNextStreamInfo(channelID string, data map[string]string) *domain.NextStreamInfo {
	if len(data) == 0 {
		return nil
	}

	info := parseCachedNextStreamInfo(data)
	if !as.hasValidNextStreamStatus(channelID, info.Status) {
		return nil
	}

	startScheduledStr := stringutil.TrimSpace(data["start_scheduled"])
	if !as.parseNextStreamStart(channelID, startScheduledStr, info) {
		return nil
	}

	if !as.hasCompleteUpcomingStreamInfo(channelID, startScheduledStr, info) {
		return nil
	}

	return info
}

func parseCachedNextStreamInfo(data map[string]string) *domain.NextStreamInfo {
	return &domain.NextStreamInfo{
		Status:  domain.NextStreamStatus(stringutil.TrimSpace(data["status"])),
		VideoID: stringutil.TrimSpace(data["video_id"]),
		Title:   stringutil.TrimSpace(data["title"]),
	}
}

func (as *AlarmService) hasValidNextStreamStatus(channelID string, status domain.NextStreamStatus) bool {
	if status.IsValid() {
		return true
	}

	as.logger.Warn("Unexpected cache status",
		slog.String("channel_id", channelID),
		slog.String("status", status.String()),
	)

	return false
}

func (as *AlarmService) parseNextStreamStart(channelID, startScheduledStr string, info *domain.NextStreamInfo) bool {
	if startScheduledStr == "" {
		return true
	}

	scheduledDate, err := time.Parse(time.RFC3339, startScheduledStr)
	if err != nil {
		as.logger.Error("Failed to parse scheduled time",
			slog.String("channel_id", channelID),
			slog.String("start_scheduled", startScheduledStr),
			slog.Any("error", err),
		)

		return false
	}

	info.StartScheduled = &scheduledDate

	return true
}

func (as *AlarmService) hasCompleteUpcomingStreamInfo(
	channelID, startScheduledStr string,
	info *domain.NextStreamInfo,
) bool {
	if !info.Status.IsUpcoming() {
		return true
	}

	if startScheduledStr != "" && info.Title != "" && info.VideoID != "" && info.StartScheduled != nil {
		return true
	}

	as.logger.Error("Incomplete cache data for upcoming stream",
		slog.String("channel_id", channelID),
		slog.Bool("has_title", info.Title != ""),
		slog.Bool("has_start", startScheduledStr != ""),
		slog.Bool("has_video_id", info.VideoID != ""),
	)

	return false
}

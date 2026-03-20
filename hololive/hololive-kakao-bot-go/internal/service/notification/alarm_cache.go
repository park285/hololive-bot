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

package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/valkey-io/valkey-go"
)

// CacheMemberName: 채널 ID에 해당하는 멤버 이름을 Redis에 캐싱한다. (표시 이름 최적화).
func (as *AlarmService) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	if err := as.cache.HSet(ctx, MemberNameKey, channelID, memberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	return nil
}

// GetMemberName: 캐시된 멤버 이름을 조회한다. 없으면 빈 문자열을 반환합니다.
func (as *AlarmService) GetMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := as.cache.HGet(ctx, MemberNameKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}

	return name, nil
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	key := as.channelSubscribersKeyByType(channelID, alarmType)

	subscribers, err := as.cache.SMembers(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get channel subscribers by type: %w", err)
	}

	return subscribers, nil
}

// SetRoomName: 방 ID에 대한 표시 이름을 설정합니다.
func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	if err := as.cache.HSet(ctx, RoomNamesCacheKey, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}

// SetUserName: 사용자 ID에 대한 표시 이름을 설정합니다.
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
	normalized := stringutil.NormalizeKey(title)
	if normalized == "" {
		normalized = stringutil.NormalizeKey(streamID)
	}

	if normalized == "" {
		normalized = "untitled"
	}

	sum := sha256.Sum256([]byte(normalized))

	return hex.EncodeToString(sum[:8])
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

// MarkAsNotified: 해당 방송(streamID)에 대해 특정 시점(minutesUntil)의 알림을 발송했음을 기록합니다.
// read-modify-write: 기존 데이터 조회 → 스케줄 변경 시 맵 리셋 → 플래그 추가 → 저장.
//
// 병렬 안전성: workerPool에서 동일 streamID에 대해 여러 room이 동시 호출할 수 있으나,
// 같은 체크 주기에서 동일 streamID는 동일 minutesUntil을 가지므로 write 내용이 동일하여
// 데이터 손실 없음 (benign race).
func (as *AlarmService) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	notifiedKey := NotifiedKeyPrefix + streamID
	scheduledStr := normalizeScheduledMinute(startScheduled).Format(time.RFC3339)

	// 기존 데이터 조회
	var existing NotifiedData
	if err := as.cache.Get(ctx, notifiedKey, &existing); err != nil || existing.StartScheduled == "" {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}

	// 스케줄 변경 시 맵 리셋
	if existing.StartScheduled != scheduledStr {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}

	if existing.SentAt == nil {
		existing.SentAt = make(map[int]bool)
	}

	existing.SentAt[minutesUntil] = true

	if err := as.cache.Set(ctx, notifiedKey, existing, constants.CacheTTL.NotificationSent); err != nil {
		as.logger.Warn("Failed to mark as notified",
			slog.String("stream_id", streamID),
			slog.Any("error", err),
		)

		return fmt.Errorf("mark as notified: %w", err)
	}

	return nil
}

// MarkUpcomingEventNotified: 예정 알림 발송 시각을 이벤트 단위로 기록합니다.
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

// WasUpcomingEventNotifiedRecently: 동일 이벤트의 예정 알림이 최근 window 내에 발송됐는지 확인합니다.
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

// GetNextStreamInfo: 특정 채널의 다음 방송 정보(예정 또는 라이브)를 캐시에서 조회합니다.
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

	builder := as.cache.Builder()

	cmds := make([]valkey.Completed, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		cmds = append(cmds, builder.Hget().Key(MemberNameKey).Field(channelID).Build())
	}

	results := as.cache.DoMulti(ctx, cmds...)
	names := make(map[string]string, len(channelIDs))

	for i, result := range results {
		if err := result.Error(); err != nil {
			continue
		}

		name, err := result.ToString()
		if err != nil || stringutil.TrimSpace(name) == "" {
			continue
		}

		names[channelIDs[i]] = name
	}

	return names, nil
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

	info := &domain.NextStreamInfo{
		Status:  domain.NextStreamStatus(stringutil.TrimSpace(data["status"])),
		VideoID: stringutil.TrimSpace(data["video_id"]),
		Title:   stringutil.TrimSpace(data["title"]),
	}

	if !info.Status.IsValid() {
		as.logger.Warn("Unexpected cache status",
			slog.String("channel_id", channelID),
			slog.String("status", info.Status.String()),
		)

		return nil
	}

	startScheduledStr := stringutil.TrimSpace(data["start_scheduled"])
	if startScheduledStr != "" {
		scheduledDate, err := time.Parse(time.RFC3339, startScheduledStr)
		if err != nil {
			as.logger.Error("Failed to parse scheduled time",
				slog.String("channel_id", channelID),
				slog.String("start_scheduled", startScheduledStr),
				slog.Any("error", err),
			)

			return nil
		}

		info.StartScheduled = &scheduledDate
	}

	if info.Status.IsUpcoming() {
		if startScheduledStr == "" || info.Title == "" || info.VideoID == "" || info.StartScheduled == nil {
			as.logger.Error("Incomplete cache data for upcoming stream",
				slog.String("channel_id", channelID),
				slog.Bool("has_title", info.Title != ""),
				slog.Bool("has_start", startScheduledStr != ""),
				slog.Bool("has_video_id", info.VideoID != ""),
			)

			return nil
		}
	}

	return info
}

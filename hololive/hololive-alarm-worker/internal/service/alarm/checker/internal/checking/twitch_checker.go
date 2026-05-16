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

package checking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const twitchLiveNotifiedKeyPrefix = "notified:twitch:live:"

// TwitchChecker는 Twitch 라이브 상태를 배치 조회해 알림 후보를 만든다.
type TwitchChecker struct {
	cacheSvc     cache.Client
	twitchClient *twitch.Client
	logger       *slog.Logger
}

// NewTwitchChecker는 Twitch 체커를 생성한다.
func NewTwitchChecker(cacheSvc cache.Client, twitchClient *twitch.Client, logger *slog.Logger) (*TwitchChecker, error) {
	if cacheSvc == nil {
		return nil, errors.New("new twitch checker: cache service is nil")
	}

	if twitchClient == nil {
		return nil, errors.New("new twitch checker: twitch client is nil")
	}

	return &TwitchChecker{
		cacheSvc:     cacheSvc,
		twitchClient: twitchClient,
		logger:       safeLogger(logger),
	}, nil
}

// Check는 alarm:twitch_logins 매핑 기반으로 Twitch 라이브 알림 후보를 생성한다.
func (c *TwitchChecker) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	loginMappingsRaw, err := c.cacheSvc.HGetAll(ctx, sharedalarmkeys.TwitchLoginMapKey)
	if err != nil {
		return nil, fmt.Errorf("check twitch streams: read login mappings: %w", err)
	}

	loginMappings, youtubeChannelIDs := normalizeTwitchLoginMappings(loginMappingsRaw)
	if len(loginMappings) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	subscriberMap, err := loadSubscriberRoomsByChannel(ctx, c.cacheSvc, youtubeChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("check twitch streams: load subscriber rooms: %w", err)
	}

	loginsToLookup := buildTwitchLookupLogins(loginMappings, subscriberMap)
	if len(loginsToLookup) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	streamsResponse, err := c.twitchClient.GetStreams(ctx, loginsToLookup)
	if err != nil {
		return nil, fmt.Errorf("check twitch streams: get streams batch: %w", err)
	}

	if streamsResponse == nil || len(streamsResponse.Data) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	notifications, err := c.buildLiveNotifications(ctx, loginMappings, subscriberMap, streamsResponse)
	if err != nil {
		return nil, fmt.Errorf("check twitch streams: build live notifications: %w", err)
	}

	return notifications, nil
}

func normalizeTwitchLoginMappings(loginMappingsRaw map[string]string) (map[string]string, []string) {
	if len(loginMappingsRaw) == 0 {
		return map[string]string{}, []string{}
	}

	loginMappings := make(map[string]string, len(loginMappingsRaw))

	youtubeChannelIDs := make([]string, 0, len(loginMappingsRaw))
	for login, channelID := range loginMappingsRaw {
		normalizedLogin := stringutil.Normalize(login)

		trimmedChannelID := strings.TrimSpace(channelID)
		if normalizedLogin == "" || trimmedChannelID == "" {
			continue
		}

		loginMappings[normalizedLogin] = trimmedChannelID
		youtubeChannelIDs = append(youtubeChannelIDs, trimmedChannelID)
	}

	return loginMappings, youtubeChannelIDs
}

func buildTwitchLookupLogins(loginMappings map[string]string, subscriberMap map[string][]string) []string {
	loginsToLookup := make([]string, 0, len(loginMappings))
	for login, channelID := range loginMappings {
		if len(subscriberMap[channelID]) == 0 {
			continue
		}

		loginsToLookup = append(loginsToLookup, login)
	}

	return uniqueStrings(loginsToLookup)
}

func (c *TwitchChecker) buildLiveNotifications(
	_ context.Context,
	loginMappings map[string]string,
	subscriberMap map[string][]string,
	streamsResponse *twitch.StreamsResponse,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0)

	for idx := range streamsResponse.Data {
		streamData := &streamsResponse.Data[idx]
		if !streamData.IsLive() {
			continue
		}

		notifications = append(notifications, buildTwitchStreamNotifications(streamData, loginMappings, subscriberMap)...)
	}

	return notifications, nil
}

func buildTwitchStreamNotifications(
	streamData *twitch.StreamData,
	loginMappings map[string]string,
	subscriberMap map[string][]string,
) []*domain.AlarmNotification {
	normalizedLogin := stringutil.Normalize(streamData.UserLogin)

	youtubeChannelID, ok := loginMappings[normalizedLogin]
	if !ok {
		return nil
	}

	subscriberRooms := subscriberMap[youtubeChannelID]
	if len(subscriberRooms) == 0 {
		return nil
	}

	stream := buildTwitchLiveStream(youtubeChannelID, streamData)
	if stream == nil {
		return nil
	}

	return roomNotifications(subscriberRooms, stream.Channel, stream, 0, "")
}

// buildTwitchLiveDedupKey는 이전 checker-level preclaim 테스트 호환을 위해 남겨둔다.
// 실제 dedup claim은 Notifier가 처리한다.
func buildTwitchLiveDedupKey(userID, streamID string) string {
	return fmt.Sprintf("%s%s:%s", twitchLiveNotifiedKeyPrefix, userID, streamID)
}

func buildTwitchLiveStream(youtubeChannelID string, streamData *twitch.StreamData) *domain.Stream {
	if streamData == nil {
		return nil
	}

	startAt := streamData.StartedAt.UTC()
	startScheduled := startAt

	channelName := strings.TrimSpace(streamData.UserName)
	if channelName == "" {
		channelName = strings.TrimSpace(streamData.UserLogin)
	}

	title := strings.TrimSpace(streamData.Title)
	if title == "" {
		title = "Twitch 라이브"
	}

	normalizedLogin := stringutil.Normalize(streamData.UserLogin)

	twitchUserID := strings.TrimSpace(streamData.UserID)
	if twitchUserID == "" {
		twitchUserID = normalizedLogin
	}

	twitchStreamID := strings.TrimSpace(streamData.ID)
	if twitchStreamID == "" {
		twitchStreamID = startAt.Format("20060102T150405Z")
	}

	viewerCount := streamData.ViewerCount
	liveURL := ""

	if normalizedLogin != "" {
		liveURL = fmt.Sprintf("https://twitch.tv/%s", normalizedLogin)
	}

	return &domain.Stream{
		ID:             fmt.Sprintf("twitch:%s:%s", twitchUserID, twitchStreamID),
		Title:          title,
		ChannelID:      youtubeChannelID,
		ChannelName:    channelName,
		Status:         domain.StreamStatusLive,
		StartScheduled: &startScheduled,
		StartActual:    &startAt,
		ViewerCount:    &viewerCount,
		Channel: &domain.Channel{
			ID:   youtubeChannelID,
			Name: channelName,
		},
		TwitchUserID:    twitchUserID,
		TwitchUserLogin: normalizedLogin,
		TwitchStreamID:  twitchStreamID,
		TwitchLiveURL:   liveURL,
		IsTwitchOnly:    true,
	}
}

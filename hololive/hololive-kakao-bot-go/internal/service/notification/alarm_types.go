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
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// 알람 키 상수 목록.
const (
	AlarmKeyPrefix              = sharedalarmkeys.AlarmKeyPrefix
	AlarmRegistryKey            = sharedalarmkeys.AlarmRegistryKey
	AlarmChannelRegistryKey     = sharedalarmkeys.AlarmChannelRegistryKey
	ChannelSubscribersKeyPrefix = sharedalarmkeys.ChannelSubscribersKeyPrefix
	ChzzkChannelMapKey          = "alarm:chzzk_channels"
	TwitchLoginMapKey           = "alarm:twitch_logins"
	TwitchChannelLoginMapKey    = "alarm:twitch_channel_logins"
	MemberNameKey               = sharedalarmkeys.MemberNameKey
	RoomNamesCacheKey           = sharedalarmkeys.RoomNamesCacheKey
	UserNamesCacheKey           = sharedalarmkeys.UserNamesCacheKey
	NotifiedKeyPrefix           = "notified:"
	NotifyClaimKeyPrefix        = "notified:claim:"
	NotifyLogicalClaimKeyPrefix = "notified:claim:event:"
	UpcomingEventKeyPrefix      = "notified:upcoming:event:"
	ScheduleTransitionKeyPrefix = "notified:schedule:transition:"
	NextStreamKeyPrefix         = "alarm:next_stream:"

	// 타입별 구독자 키 접두사 (COMMUNITY, SHORTS용).
	ChannelSubscribersCommunityPrefix = sharedalarmkeys.ChannelSubscribersCommunityPrefix
	ChannelSubscribersShortsPrefix    = sharedalarmkeys.ChannelSubscribersShortsPrefix

	// Chzzk 알림 키 접두사.
	ChzzkLiveNotifiedKeyPrefix  = "notified:chzzk:live:"
	IntegratedNotifiedKeyPrefix = "notified:integrated:"
)

// 스케줄 변경 시 StartScheduled 불일치 → SentAt 맵 리셋.
type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

type alarmWriter interface {
	Add(ctx context.Context, alarm *domain.Alarm) error
	Remove(ctx context.Context, roomID, channelID string) error
	ClearByRoom(ctx context.Context, roomID string) (int64, error)
}

type AlarmService struct {
	cache           cache.Client
	holodex         *holodex.Service
	chzzk           *chzzk.Client
	twitch          *twitch.Client
	memberData      domain.MemberDataProvider
	alarmRepo       *alarm.Repository
	alarmWriter     alarmWriter
	logger          *slog.Logger
	targetPolicy    sharedchecker.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	platformMapMu   sync.Mutex
}

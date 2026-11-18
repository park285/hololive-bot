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

package handlercore

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error
}

type CelebrationCalendarFinder interface {
	FindMembersWithCelebrationsInMonth(ctx context.Context, month, referenceYear int) ([]domain.CalendarEntry, error)
}

type CalendarImageRenderer interface {
	RenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error)
}

type HelpImageProvider interface {
	HelpImages(ctx context.Context) ([][]byte, error)
}

// SchedulePosterReader의 day는 JST 날짜다.
type SchedulePosterReader interface {
	MemberCardTemplate(ctx context.Context, memberID int) (cardtemplate.ID, error)
	PublishedRows(ctx context.Context, channelID string, day time.Time) (scheduleposter.PublishedSchedule, bool, error)
}

// ScheduleCardRenderer는 확정본 행을 전송 순서대로 PNG 페이지에 담는다.
type ScheduleCardRenderer interface {
	RenderScheduleCards(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID) ([][]byte, error)
}

type BroadcastHistoryQuery struct {
	ChannelID string
	Type      string
	TopicID   string
	Limit     int
	Since     time.Time
}

type BroadcastThumbnailQuery struct {
	VideoID string
}

type BroadcastHistoryEntry struct {
	VideoID            string
	ChannelID          string
	MemberName         string
	Title              string
	TopicID            string
	ThumbnailURL       string
	BroadcastType      string
	ScheduledStartTime *time.Time
	StartedAt          *time.Time
	EndedAt            *time.Time
	LastSeenAt         time.Time
	// LifecycleOrigin은 youtube_live_sessions.lifecycle_origin이다. legacy_unknown은 수명 추적 도입 전 기록이다.
	LifecycleOrigin string
	// LastLivePositiveAt은 마지막으로 방송 중임을 확인한 시각이다. reconciliation head가 없거나 LIVE를 확인하지 못한
	// 세션은 nil이다.
	LastLivePositiveAt  *time.Time
	TimesSource         string
	FinalizationOutcome *string
	FinalizationReason  *string
}

type BroadcastHistoryResult struct {
	Entries   []BroadcastHistoryEntry
	Truncated bool
}

// GetEndedBroadcast는 조회 대상이 없을 때 ErrBroadcastNotFound를 반환한다.
var ErrBroadcastNotFound = errors.New("broadcast not found")

type BroadcastHistoryRepository interface {
	ListEndedBroadcasts(ctx context.Context, query *BroadcastHistoryQuery) (BroadcastHistoryResult, error)
	GetEndedBroadcast(ctx context.Context, query BroadcastThumbnailQuery) (*BroadcastHistoryEntry, error)
}

// ErrBroadcastThumbnailNotFound는 Download가 모든 후보 URL에서 HTTP 404를 받았을 때 감싸는 오류다.
// 다른 실패가 하나라도 섞이면 감싸지 않는다.
var ErrBroadcastThumbnailNotFound = errors.New("broadcast thumbnail not found")

type BroadcastThumbnailDownloader interface {
	Download(ctx context.Context, entry *BroadcastHistoryEntry) ([]byte, string, error)
}

// AlarmService는 bot 명령이 소비하는 알림 저장 작업만 제공한다.
type AlarmService interface {
	AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error)
	RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error)
	RemoveHostAlarm(context.Context, string, string, string, domain.AlarmTypes) (bool, error)
	ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error)
	ClearRoomAlarms(context.Context, string) (int, error)
}

// CollabMemberReader는 방송 목록 명령이 소비하는 콜라보 출연자 표시명 조회다.
type CollabMemberReader interface {
	DisplayNames(ctx context.Context, videos []collabmembers.Video) (map[string][]string, error)
}

type Dependencies struct {
	Holodex             domain.StreamProvider
	LiveQuery           livequery.Reader
	CollabMembers       CollabMemberReader
	Alarm               AlarmService
	Matcher             *matcher.Matcher
	BroadcastHistory    BroadcastHistoryRepository
	ThumbnailDownloader BroadcastThumbnailDownloader
	MembersData         domain.MemberDataProvider
	Formatter           *formatter.ResponseFormatter
	HelpImageProvider   HelpImageProvider
	SchedulePosters     SchedulePosterReader
	ScheduleCards       ScheduleCardRenderer
	SendMessage         func(ctx context.Context, room, message string) error
	SendImage           func(ctx context.Context, room string, imageData []byte) error
	SendTextFile        func(ctx context.Context, room, fileName, text string) error
	SendImages          func(ctx context.Context, room string, images [][]byte) error
	SendError           func(ctx context.Context, room, message string) error
	Logger              *slog.Logger
}

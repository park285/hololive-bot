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
	"log/slog"

	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error
}

type Event struct {
	Type   domain.CommandType
	Params map[string]any
}

type Dispatcher interface {
	Publish(ctx context.Context, cmdCtx *domain.CommandContext, events ...Event) (int, error)
}

type MemberNewsService interface {
	GenerateRoomDigest(ctx context.Context, roomID string, period membernewscontracts.Period) (*membernewscontracts.Digest, error)
	SubscribeRoom(ctx context.Context, roomID, roomName string) error
	UnsubscribeRoom(ctx context.Context, roomID string) error
	IsRoomSubscribed(ctx context.Context, roomID string) (bool, error)
}

type MajorEventRepository interface {
	IsSubscribed(ctx context.Context, roomID string) (bool, error)
	Subscribe(ctx context.Context, roomID, roomName string) error
	Unsubscribe(ctx context.Context, roomID string) error
}

type CelebrationCalendarFinder interface {
	FindMembersWithCelebrationsInMonth(ctx context.Context, month, referenceYear int) ([]domain.CalendarEntry, error)
}

type CalendarImageRenderer interface {
	RenderCalendarImages(month, year int, entries []domain.CalendarEntry) ([][]byte, error)
}

type LiveImageRenderer interface {
	RenderLiveImages(entries []render.LiveCardEntry) ([][]byte, error)
}

type Dependencies struct {
	Holodex            domain.StreamProvider
	Chzzk              *chzzk.Client
	Cache              cache.Client
	Alarm              domain.AlarmCRUD
	Matcher            *matcher.Matcher
	OfficialProfiles   *member.ProfileService
	StatsRepository    stats.StatsCommandRepository
	MemberNews         MemberNewsService
	MembersData        member.DataProvider
	Formatter          *adapter.ResponseFormatter
	SendMessage        func(ctx context.Context, room, message string) error
	SendImage          func(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) error
	SendMultipleImages func(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) error
	SendError          func(ctx context.Context, room, message string) error
	Dispatcher         Dispatcher
	Logger             *slog.Logger
}

type NormalizeFunc func(domain.CommandType, map[string]any) (string, map[string]any)

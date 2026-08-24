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
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmcache"
	"github.com/kapu/hololive-shared/pkg/service/notification/platformmap"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

type alarmWriter interface {
	Add(ctx context.Context, alarm *domain.Alarm) error
	Remove(ctx context.Context, roomID, channelID string) error
	ClearByRoom(ctx context.Context, roomID string) (int64, error)
}

type AlarmService struct {
	cache           cache.Client
	holodex         *holodexprovider.Service
	chzzk           *chzzk.Client
	twitch          *twitch.Client
	memberData      domain.MemberDataProvider
	alarmRepository *alarm.Repository
	alarmWriter     alarmWriter
	logger          *slog.Logger
	targetPolicy    sharedchecker.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	cacheState      *alarmcache.State
	platformMapper  *platformmap.Mapper
}

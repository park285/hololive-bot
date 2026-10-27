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

package subscriptions

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions/internal/alarmcache"
	targetpolicy "github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type alarmWriter interface {
	Add(ctx context.Context, alarm *domain.Alarm) error
	Remove(ctx context.Context, roomID, channelID string) error
	RemoveHost(ctx context.Context, roomID, channelID, hostID string) error
	ClearByRoom(ctx context.Context, roomID string) (int64, error)
}

// Cache는 AlarmService가 구독 cache 증분 동기화(Set·Del·SAdd·SRem·Lua Exec), 전체 rebuild, 멤버 이름 hash에 쓰는
// cache 작업이다. ScriptCache와 연결 관리(Close·IsConnected·WaitUntilReady)는 쓰지 않는다.
type Cache interface {
	cache.DataCache
	cache.LowLevelCache
}

type AlarmService struct {
	cache           Cache
	memberData      domain.MemberDataProvider
	alarmRepository *alarmstore.Repository
	alarmWriter     alarmWriter
	logger          *slog.Logger
	targetPolicy    targetpolicy.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	cacheMutationMu sync.Mutex
	cacheState      *alarmcache.State
}

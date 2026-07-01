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

package milestonescheduler

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type Service = youtube.Service

type ChannelStats = youtube.ChannelStats

type Scheduler = youtube.Scheduler

type MilestoneMessageFormatter = youtube.MilestoneMessageFormatter

type schedulerImpl struct {
	youtube              Service
	holodex              *holodex.Service
	cache                cache.KeyValueCache
	statsRepository      ytstats.StatsSchedulerRepository
	membersData          domain.MemberDataProvider
	alarmService         domain.AlarmDispatchState
	irisClient           iris.Sender
	formatter            MilestoneMessageFormatter
	logger               *slog.Logger
	ticker               *time.Ticker
	milestoneWatchTicker *time.Ticker
	stopCh               chan struct{}
	stopOnce             sync.Once
	currentBatch         int
	batchMu              sync.Mutex
	batchRunning         atomic.Bool
	alertSentRooms       sentRoomLedger
}

const (
	schedulerInterval         = 12 * time.Hour
	milestoneWatchInterval    = 1 * time.Hour // 마일스톤 직전 멤버 빠른 체크
	MilestoneThresholdRatio   = youtube.MilestoneThresholdRatio
	ApproachingThresholdRatio = youtube.ApproachingThresholdRatio

	channelsPerBatch             = 30 // 30 channel × 100 unit = batch당 3,000 unit
	batchesPerDay                = 2  // batch 2회 × 3,000 = 6,000 unit
	totalDailyQuota              = 6000
	recentVideosFetchParallelism = 4
)

var SubscriberMilestones = youtube.SubscriberMilestones

func NewScheduler(
	youtubeService Service,
	holodexService *holodex.Service,
	cacheClient cache.KeyValueCache,
	statsRepository ytstats.StatsSchedulerRepository,
	membersData domain.MemberDataProvider,
	alarmService domain.AlarmDispatchState,
	irisClient iris.Sender,
	formatter MilestoneMessageFormatter,
	logger *slog.Logger,
) Scheduler {
	return &schedulerImpl{
		youtube:         youtubeService,
		holodex:         holodexService,
		cache:           cacheClient,
		statsRepository: statsRepository,
		membersData:     membersData,
		alarmService:    alarmService,
		irisClient:      irisClient,
		formatter:       formatter,
		logger:          logger,
		currentBatch:    0,
		stopCh:          make(chan struct{}),
	}
}

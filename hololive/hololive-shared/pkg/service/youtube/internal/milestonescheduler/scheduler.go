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
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	apiservice "github.com/kapu/hololive-shared/pkg/service/youtube/internal/apiservice"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type Service = apiservice.Service

type ChannelStats = apiservice.ChannelStats

type Scheduler interface {
	Start(ctx context.Context)
	Stop()
}

type MilestoneMessageFormatter interface {
	FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error)
	FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error)
}

type schedulerImpl struct {
	youtube              Service
	holodex              *holodex.Service
	cache                cache.Client
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
}

const (
	schedulerInterval         = 12 * time.Hour
	milestoneWatchInterval    = 1 * time.Hour // 마일스톤 직전 멤버 빠른 체크
	MilestoneThresholdRatio   = 0.95          // 95% 이상이면 마일스톤 직전으로 간주
	ApproachingThresholdRatio = 0.99          // 99% 이상이면 예고 알람 발송

	channelsPerBatch             = 30 // 30 channels × 100 units = 3,000 units per batch
	batchesPerDay                = 2  // 2 batches × 3,000 = 6,000 units
	totalDailyQuota              = 6000
	recentVideosFetchParallelism = 4
)

var SubscriberMilestones = []uint64{
	100000, 250000, 500000, 750000, 1000000,
	1500000, 2000000, 2500000, 3000000, 4000000, 5000000,
}

func NewScheduler(
	youtubeService Service,
	holodexService *holodex.Service,
	cacheClient cache.Client,
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

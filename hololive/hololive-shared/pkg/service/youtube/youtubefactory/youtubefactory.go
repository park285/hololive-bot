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

package youtubefactory

import (
	"context"
	"log/slog"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	apiservice "github.com/kapu/hololive-shared/pkg/service/youtube/internal/apiservice"
	milestonescheduler "github.com/kapu/hololive-shared/pkg/service/youtube/internal/milestonescheduler"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

func NewYouTubeService(
	ctx context.Context,
	apiKey string,
	cacheClient cache.Client,
	scraperProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) (youtube.Service, error) {
	return apiservice.New(ctx, apiKey, cacheClient, scraperProxyConfig, sharedRL, logger)
}

func NewScheduler(
	youtubeService youtube.Service,
	holodexService *holodex.Service,
	cacheClient cache.Client,
	statsRepository ytstats.StatsSchedulerRepository,
	membersData domain.MemberDataProvider,
	alarmService domain.AlarmDispatchState,
	irisClient iris.Sender,
	formatter youtube.MilestoneMessageFormatter,
	logger *slog.Logger,
) youtube.Scheduler {
	return milestonescheduler.NewScheduler(
		youtubeService,
		holodexService,
		cacheClient,
		statsRepository,
		membersData,
		alarmService,
		irisClient,
		formatter,
		logger,
	)
}

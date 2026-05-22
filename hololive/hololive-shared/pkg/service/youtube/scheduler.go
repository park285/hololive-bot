package youtube

import (
	"log/slog"

	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	milestonescheduler "github.com/kapu/hololive-shared/pkg/service/youtube/internal/milestonescheduler"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type MilestoneMessageFormatter = milestonescheduler.MilestoneMessageFormatter

const (
	MilestoneThresholdRatio   = milestonescheduler.MilestoneThresholdRatio
	ApproachingThresholdRatio = milestonescheduler.ApproachingThresholdRatio
)

var SubscriberMilestones = milestonescheduler.SubscriberMilestones

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

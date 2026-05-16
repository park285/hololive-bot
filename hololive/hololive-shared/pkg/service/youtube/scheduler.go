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
	youtubeSvc Service,
	holodexSvc *holodex.Service,
	cacheSvc cache.Client,
	statsRepo ytstats.StatsSchedulerRepository,
	membersData domain.MemberDataProvider,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Sender,
	formatter MilestoneMessageFormatter,
	logger *slog.Logger,
) Scheduler {
	return milestonescheduler.NewScheduler(
		youtubeSvc,
		holodexSvc,
		cacheSvc,
		statsRepo,
		membersData,
		alarmSvc,
		irisClient,
		formatter,
		logger,
	)
}

package youtube

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

// NOTE:
// stats_repository_* 구현은 youtube/stats 서브패키지로 이동했다.
// 기존 import path(youtube 패키지) 호환을 위해 필요한 타입/함수를 alias로 re-export 한다.

// Repository + constructors
type StatsRepository = stats.StatsRepository

func NewYouTubeStatsRepository(postgres database.Client, logger *slog.Logger) *StatsRepository {
	return stats.NewYouTubeStatsRepository(postgres, logger)
}

// Interfaces
type StatsWriteRepository = stats.StatsWriteRepository
type StatsReadRepository = stats.StatsReadRepository
type MilestoneRepository = stats.MilestoneRepository
type SubscriberGraphRepository = stats.SubscriberGraphRepository
type NotificationRepository = stats.NotificationRepository

type StatsServiceRepository = stats.StatsServiceRepository
type StatsSchedulerRepository = stats.StatsSchedulerRepository
type StatsCommandRepository = stats.StatsCommandRepository
type StatsDashboardRepository = stats.StatsDashboardRepository

// DTO / API models
type MilestoneEntry = stats.MilestoneEntry
type MilestoneFilter = stats.MilestoneFilter
type MilestoneResult = stats.MilestoneResult
type NearMilestoneEntry = stats.NearMilestoneEntry
type MilestoneStats = stats.MilestoneStats

type SubscriberGraphPoint = stats.SubscriberGraphPoint
type SubscriberGraphData = stats.SubscriberGraphData

type ApproachingNotification = stats.ApproachingNotification
type MilestoneNotification = stats.MilestoneNotification

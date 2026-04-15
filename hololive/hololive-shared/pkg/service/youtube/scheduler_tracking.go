package youtube

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type channelStatsWorkItem struct {
	channelID        string
	member           *domain.Member
	currentStats     *ChannelStats
	prevStats        *domain.TimestampedStats
	timestampedStats *domain.TimestampedStats
}

type subscriberTrackingResult struct {
	totalChanges              int
	totalMilestones           int
	totalRecordErrors         int
	totalSaveErrors           int
	totalGetLatestErrors      int
	totalMilestoneCheckErrors int
	totalMilestoneSaveErrors  int
	fetchErrors               int
	tracked                   int
}

type preparedWorkItemsResult struct {
	workItems                 []channelStatsWorkItem
	statsBatch                []*domain.TimestampedStats
	latestErrors              int
	milestoneCheckErrors      int
	milestonesByChannel       map[string][]uint64
	milestonePreloadAvailable bool
}

type changeProcessingResult struct {
	changes              int
	milestones           int
	recordErrors         int
	milestoneCheckErrors int
	milestoneSaveErrors  int
}

type channelStatsResult struct {
	changesDetected      int
	milestonesAchieved   int
	milestoneCheckErrors int
	milestoneSaveErrors  int
	change               *domain.StatsChange
}

func (ys *schedulerImpl) trackAllSubscribers(ctx context.Context) {
	channelIDs, channelToMember := ys.buildChannelMaps()

	ys.logger.Info("Tracking all member subscribers",
		slog.Int("channels", len(channelIDs)),
		slog.Int("quota_cost", len(channelIDs)))

	stats, err := ys.youtube.GetChannelStatistics(ctx, channelIDs)
	var r subscriberTrackingResult
	if err != nil {
		r.fetchErrors = len(channelIDs)
		stats = make(map[string]*ChannelStats, 0)
	}
	r.tracked = len(stats)

	now := time.Now()
	prepared := ys.prepareWorkItems(ctx, stats, channelToMember, now)
	r.totalGetLatestErrors = prepared.latestErrors
	r.totalMilestoneCheckErrors = prepared.milestoneCheckErrors

	if len(prepared.statsBatch) > 0 {
		if err := ys.statsRepo.SaveStatsBatch(ctx, prepared.statsBatch); err != nil {
			r.totalSaveErrors = len(prepared.statsBatch)
			ys.logger.Warn("Failed to batch save subscriber stats",
				slog.Int("count", len(prepared.statsBatch)),
				slog.Any("error", err))
		} else {
			cr := ys.processAndRecordChanges(
				ctx,
				prepared.workItems,
				prepared.milestonesByChannel,
				prepared.milestonePreloadAvailable,
				now,
			)
			r.totalChanges = cr.changes
			r.totalMilestones = cr.milestones
			r.totalRecordErrors = cr.recordErrors
			r.totalMilestoneCheckErrors += cr.milestoneCheckErrors
			r.totalMilestoneSaveErrors = cr.milestoneSaveErrors
		}
	}

	ys.logger.Info("Subscriber tracking completed",
		slog.Int("tracked", r.tracked),
		slog.Int("changes", r.totalChanges),
		slog.Int("milestones", r.totalMilestones),
		slog.Int("record_errors", r.totalRecordErrors),
		slog.Int("save_errors", r.totalSaveErrors),
		slog.Int("latest_stats_errors", r.totalGetLatestErrors),
		slog.Int("milestone_check_errors", r.totalMilestoneCheckErrors),
		slog.Int("milestone_save_errors", r.totalMilestoneSaveErrors),
		slog.Int("fetch_errors", r.fetchErrors))
}

func (ys *schedulerImpl) prepareWorkItems(
	ctx context.Context,
	stats map[string]*ChannelStats,
	channelToMember map[string]*domain.Member,
	now time.Time,
) preparedWorkItemsResult {
	var latestErrors, milestoneCheckErrors int
	milestonePreloadAvailable := true

	latestStatsByChannel, latestErr := ys.statsRepo.GetLatestStatsForChannels(ctx, mapKeys(stats))
	if latestErr != nil {
		latestErrors = len(stats)
		latestStatsByChannel = make(map[string]*domain.TimestampedStats)
	}

	milestonesByChannel, milestoneBatchErr := ys.statsRepo.GetAchievedMilestones(ctx, mapKeys(stats), domain.MilestoneSubscribers)
	if milestoneBatchErr != nil {
		milestoneCheckErrors = len(stats)
		milestonesByChannel = make(map[string][]uint64)
		milestonePreloadAvailable = false
	}

	workItems := make([]channelStatsWorkItem, 0, len(stats))
	statsBatch := make([]*domain.TimestampedStats, 0, len(stats))

	for channelID, currentStats := range stats {
		member := channelToMember[channelID]
		if member == nil {
			continue
		}

		prevStats := latestStatsByChannel[channelID]
		timestampedStats := createTimestampedStats(channelID, member, currentStats, now)
		workItems = append(workItems, channelStatsWorkItem{
			channelID:        channelID,
			member:           member,
			currentStats:     currentStats,
			prevStats:        prevStats,
			timestampedStats: timestampedStats,
		})
		statsBatch = append(statsBatch, timestampedStats)
	}

	return preparedWorkItemsResult{
		workItems:                 workItems,
		statsBatch:                statsBatch,
		latestErrors:              latestErrors,
		milestoneCheckErrors:      milestoneCheckErrors,
		milestonesByChannel:       milestonesByChannel,
		milestonePreloadAvailable: milestonePreloadAvailable,
	}
}

func (ys *schedulerImpl) processAndRecordChanges(
	ctx context.Context,
	workItems []channelStatsWorkItem,
	milestonesByChannel map[string][]uint64,
	milestonePreloadAvailable bool,
	now time.Time,
) changeProcessingResult {
	var cr changeProcessingResult
	var changeBatch []*domain.StatsChange

	for _, item := range workItems {
		r := ys.processChannelStats(
			ctx,
			item.channelID,
			item.member,
			item.currentStats,
			item.prevStats,
			item.timestampedStats,
			milestonesByChannel[item.channelID],
			milestonePreloadAvailable,
			now,
		)
		if r.change != nil {
			changeBatch = append(changeBatch, r.change)
		}
		cr.changes += r.changesDetected
		cr.milestones += r.milestonesAchieved
		cr.milestoneCheckErrors += r.milestoneCheckErrors
		cr.milestoneSaveErrors += r.milestoneSaveErrors
	}

	if len(changeBatch) > 0 {
		if err := ys.statsRepo.RecordChangeBatch(ctx, changeBatch); err != nil {
			cr.recordErrors = len(changeBatch)
			ys.logger.Warn("Failed to batch record changes",
				slog.Int("count", len(changeBatch)),
				slog.Any("error", err))
		}
	}

	return cr
}

func (ys *schedulerImpl) buildChannelMaps() ([]string, map[string]*domain.Member) {
	allMembers := ys.membersData.GetAllMembers()
	channelIDs := make([]string, 0, len(allMembers))
	channelToMember := make(map[string]*domain.Member)

	for _, member := range allMembers {
		if member.IsGraduated {
			continue
		}
		channelIDs = append(channelIDs, member.ChannelID)
		channelToMember[member.ChannelID] = member
	}

	return channelIDs, channelToMember
}

func createTimestampedStats(channelID string, member *domain.Member, stats *ChannelStats, timestamp time.Time) *domain.TimestampedStats {
	return &domain.TimestampedStats{
		ChannelID:       channelID,
		MemberName:      member.Name,
		SubscriberCount: stats.SubscriberCount,
		VideoCount:      stats.VideoCount,
		ViewCount:       stats.ViewCount,
		Timestamp:       timestamp,
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func (ys *schedulerImpl) processChannelStats(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	currentStats *ChannelStats,
	prevStats *domain.TimestampedStats,
	timestampedStats *domain.TimestampedStats,
	achievedMilestones []uint64,
	milestonePreloadAvailable bool,
	now time.Time,
) channelStatsResult {
	var r channelStatsResult

	if prevStats != nil {
		subChange, vidChange, viewChange := calculateStatsChanges(prevStats, currentStats)

		if subChange != 0 || vidChange != 0 {
			r.change = &domain.StatsChange{
				ChannelID:        channelID,
				MemberName:       member.Name,
				SubscriberChange: subChange,
				VideoChange:      vidChange,
				ViewChange:       viewChange,
				PreviousStats:    prevStats,
				CurrentStats:     timestampedStats,
				DetectedAt:       now,
			}
			r.changesDetected = 1

			milestones := ys.checkMilestones(prevStats.SubscriberCount, currentStats.SubscriberCount)
			r.milestonesAchieved, r.milestoneCheckErrors, r.milestoneSaveErrors = ys.processMilestones(
				ctx,
				channelID,
				member,
				milestones,
				achievedMilestones,
				milestonePreloadAvailable,
				now,
			)
		}
	}

	return r
}

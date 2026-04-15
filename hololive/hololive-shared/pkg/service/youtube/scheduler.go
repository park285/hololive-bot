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

package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/park285/iris-client-go/iris"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/kapu/hololive-shared/pkg/util"
)

// 인터페이스를 통해 의존한다.
type MilestoneMessageFormatter interface {
	FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error)
	FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error)
}

type schedulerImpl struct {
	youtube              Service
	holodex              *holodex.Service
	cache                cache.Client
	statsRepo            ytstats.StatsSchedulerRepository
	membersData          domain.MemberDataProvider
	alarmService         domain.AlarmDispatchState
	irisClient           iris.Sender
	formatter            MilestoneMessageFormatter
	logger               *slog.Logger
	ticker               *time.Ticker
	milestoneWatchTicker *time.Ticker
	stopCh               chan struct{}
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
	return &schedulerImpl{
		youtube:      youtubeSvc,
		holodex:      holodexSvc,
		cache:        cacheSvc,
		statsRepo:    statsRepo,
		membersData:  membersData,
		alarmService: alarmSvc,
		irisClient:   irisClient,
		formatter:    formatter,
		logger:       logger,
		currentBatch: 0,
		stopCh:       make(chan struct{}),
	}
}

func (ys *schedulerImpl) Start(ctx context.Context) {
	ys.ticker = time.NewTicker(schedulerInterval)

	ys.logger.Info("YouTube quota building scheduler started",
		slog.Duration("interval", schedulerInterval),
		slog.Int("channels_per_batch", channelsPerBatch),
		slog.Int("daily_quota_target", totalDailyQuota))

	// 메인 스케줄러 (12시간 간격)
	go func() {
		for {
			select {
			case <-ys.ticker.C:
				ys.runBatch(ctx)
			case <-ys.stopCh:
				ys.logger.Info("YouTube scheduler stopped")
				return
			case <-ctx.Done():
				ys.logger.Info("YouTube scheduler context canceled")
				return
			}
		}
	}()

	// 마일스톤 직전 멤버 빠른 체크 (1시간 간격, Holodex API 사용)
	if ys.holodex != nil {
		ys.milestoneWatchTicker = time.NewTicker(milestoneWatchInterval)
		ys.logger.Info("Milestone watcher started",
			slog.Duration("interval", milestoneWatchInterval),
			slog.Float64("threshold_ratio", MilestoneThresholdRatio))

		go func() {
			// 시작 직후 첫 체크 실행
			ys.watchNearMilestoneMembers(ctx)
			ys.dispatchMilestoneAlerts(ctx)

			for {
				select {
				case <-ys.milestoneWatchTicker.C:
					ys.watchNearMilestoneMembers(ctx)
					ys.dispatchMilestoneAlerts(ctx)
				case <-ys.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

func (ys *schedulerImpl) Stop() {
	if ys.ticker != nil {
		ys.ticker.Stop()
	}
	if ys.milestoneWatchTicker != nil {
		ys.milestoneWatchTicker.Stop()
	}
	close(ys.stopCh)
}

func (ys *schedulerImpl) runBatch(ctx context.Context) {
	if !ys.batchRunning.CompareAndSwap(false, true) {
		ys.logger.Warn("Skipping YouTube quota building batch: previous batch still running")
		return
	}
	defer ys.batchRunning.Store(false)

	ys.batchMu.Lock()
	batchNum := ys.currentBatch
	ys.currentBatch = (ys.currentBatch + 1) % batchesPerDay
	ys.batchMu.Unlock()

	ys.logger.Info("Running YouTube quota building batch",
		slog.Int("batch", batchNum),
		slog.Int("total_batches", batchesPerDay))

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ys.trackAllSubscribers(ctx)
	}()

	go func() {
		defer wg.Done()
		ys.fetchRecentVideosRotation(ctx, batchNum)
	}()

	wg.Wait()
}

// channelStatsWorkItem: trackAllSubscribers에서 채널별 처리를 위한 작업 단위
type channelStatsWorkItem struct {
	channelID        string
	member           *domain.Member
	currentStats     *ChannelStats
	prevStats        *domain.TimestampedStats
	timestampedStats *domain.TimestampedStats
}

// subscriberTrackingResult: trackAllSubscribers 전체 결과
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

type preparedWorkItemsResult struct {
	workItems                 []channelStatsWorkItem
	statsBatch                []*domain.TimestampedStats
	latestErrors              int
	milestoneCheckErrors      int
	milestonesByChannel       map[string][]uint64
	milestonePreloadAvailable bool
}

// prepareWorkItems: 통계 데이터를 work item과 batch 저장 대상으로 준비한다.
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

// changeProcessingResult: processAndRecordChanges의 결과
type changeProcessingResult struct {
	changes              int
	milestones           int
	recordErrors         int
	milestoneCheckErrors int
	milestoneSaveErrors  int
}

// processAndRecordChanges: work item들의 변경을 감지하고 batch INSERT한다.
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

// 멤버 데이터에서 채널 ID 리스트와 채널-멤버 맵 생성 (졸업 멤버 제외)
func (ys *schedulerImpl) buildChannelMaps() ([]string, map[string]*domain.Member) {
	allMembers := ys.membersData.GetAllMembers()
	channelIDs := make([]string, 0, len(allMembers))
	channelToMember := make(map[string]*domain.Member)

	for _, member := range allMembers {
		// 졸업 멤버는 마일스톤 추적에서 제외
		if member.IsGraduated {
			continue
		}
		channelIDs = append(channelIDs, member.ChannelID)
		channelToMember[member.ChannelID] = member
	}

	return channelIDs, channelToMember
}

// 현재 통계를 TimestampedStats 객체로 변환
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

// 이전 통계와 현재 통계를 비교하여 변경값 계산
func calculateStatsChanges(prev *domain.TimestampedStats, current *ChannelStats) (subChange, vidChange, viewChange int64) {
	subChange = int64(current.SubscriberCount) - int64(prev.SubscriberCount)
	vidChange = int64(current.VideoCount) - int64(prev.VideoCount)
	viewChange = int64(current.ViewCount) - int64(prev.ViewCount)
	return
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func buildMilestoneSet(values []uint64) map[uint64]struct{} {
	if len(values) == 0 {
		return nil
	}

	result := make(map[uint64]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

// 달성된 마일스톤들을 저장하고 달성 개수 반환 (이미 달성한 마일스톤은 건너뜀)
func (ys *schedulerImpl) processMilestones(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	milestones []uint64,
	achievedMilestones []uint64,
	milestonePreloadAvailable bool,
	now time.Time,
) (achieved int, checkErrors int, saveErrors int) {
	achieved = 0
	achievedSet := buildMilestoneSet(achievedMilestones)
	for _, milestone := range milestones {
		if milestonePreloadAvailable {
			if _, exists := achievedSet[milestone]; exists {
				ys.logger.Debug("Milestone already achieved, skipping",
					slog.String("member", member.Name),
					slog.Any("value", milestone))
				continue
			}
		} else {
			alreadyAchieved, err := ys.statsRepo.HasAchievedMilestone(ctx, channelID, domain.MilestoneSubscribers, milestone)
			if err != nil {
				checkErrors++
				continue
			}
			if alreadyAchieved {
				ys.logger.Debug("Milestone already achieved, skipping",
					slog.String("member", member.Name),
					slog.Any("value", milestone))
				continue
			}
		}

		milestoneRecord := &domain.Milestone{
			ChannelID:  channelID,
			MemberName: member.Name,
			Type:       domain.MilestoneSubscribers,
			Value:      milestone,
			AchievedAt: now,
			Notified:   false,
		}

		if err := ys.statsRepo.SaveMilestone(ctx, milestoneRecord); err != nil {
			saveErrors++
		} else {
			achieved++
			ys.logger.Info("Milestone achieved",
				slog.String("member", member.Name),
				slog.Any("subscribers", milestone))
		}
	}
	return achieved, checkErrors, saveErrors
}

// channelStatsResult: processChannelStats의 결과를 담는 구조체
type channelStatsResult struct {
	changesDetected      int
	milestonesAchieved   int
	milestoneCheckErrors int
	milestoneSaveErrors  int
	change               *domain.StatsChange // batch INSERT를 위해 수집
}

// 단일 채널의 통계 처리 (변경 감지, 마일스톤 체크). RecordChange는 caller가 batch 처리.
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

func (ys *schedulerImpl) fetchRecentVideosRotation(ctx context.Context, batchNum int) {
	channels := ys.getRotatingBatch(batchNum, channelsPerBatch)

	if len(channels) == 0 {
		ys.logger.Info("Skipping recent videos batch: no channels configured",
			slog.Int("batch", batchNum))
		return
	}

	ys.logger.Info("Fetching recent videos for batch",
		slog.Int("batch", batchNum),
		slog.Int("channels", len(channels)),
		slog.Int("quota_cost", len(channels)*100))

	successCount := 0
	errorCount := 0
	var mu sync.Mutex
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(recentVideosFetchParallelism)

	for _, channelID := range channels {
		channelID := channelID
		eg.Go(func() error {
			videos, err := ys.youtube.GetRecentVideos(egCtx, channelID, 10)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return nil
			}

			cacheKey := "youtube:recent_videos:" + channelID
			if cacheErr := ys.cache.Set(egCtx, cacheKey, videos, 24*time.Hour); cacheErr != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
				return nil
			}

			mu.Lock()
			successCount++
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait()

	ys.logger.Info("Recent videos batch completed",
		slog.Int("batch", batchNum),
		slog.Int("success", successCount),
		slog.Int("errors", errorCount))
}

func (ys *schedulerImpl) getRotatingBatch(batchNum int, size int) []string {
	allChannels := make([]string, 0, len(ys.membersData.GetAllMembers()))
	for _, member := range ys.membersData.GetAllMembers() {
		allChannels = append(allChannels, member.ChannelID)
	}

	total := len(allChannels)
	if total == 0 || size <= 0 {
		return []string{}
	}

	start := (batchNum * size) % total
	end := start + size

	if end <= total {
		return allChannels[start:end]
	}

	batch := make([]string, 0, size)
	batch = append(batch, allChannels[start:]...)
	batch = append(batch, allChannels[0:end-total]...)
	return batch
}

// checkSubscriberMilestones: 구독자 수가 마일스톤을 넘었는지 확인한다.
func (ys *schedulerImpl) checkMilestones(previous, current uint64) []uint64 {
	achieved := make([]uint64, 0, len(SubscriberMilestones))
	for _, milestone := range SubscriberMilestones {
		if previous < milestone && current >= milestone {
			achieved = append(achieved, milestone)
		}
	}

	return achieved
}

// isSignificantChange: 마일스톤 달성 여부만 체크 (구독자 증가량은 알람 대상 아님)
func (ys *schedulerImpl) isSignificantChange(change *domain.StatsChange) bool {
	if change.PreviousStats != nil && change.CurrentStats != nil {
		milestones := ys.checkMilestones(change.PreviousStats.SubscriberCount, change.CurrentStats.SubscriberCount)
		if len(milestones) > 0 {
			return true
		}
	}

	return false
}

// formatChangeMessage: 마일스톤 달성 메시지만 생성 (테스트 전용, 프로덕션은 SendMilestoneAlerts 사용)
func (ys *schedulerImpl) formatChangeMessage(change *domain.StatsChange) string {
	return ys.formatChangeMessageWithContext(context.Background(), change)
}

func (ys *schedulerImpl) formatChangeMessageWithContext(ctx context.Context, change *domain.StatsChange) string {
	if change.PreviousStats == nil || change.CurrentStats == nil {
		return ""
	}

	milestones := ys.checkMilestones(change.PreviousStats.SubscriberCount, change.CurrentStats.SubscriberCount)
	if len(milestones) > 0 {
		milestone := milestones[0]
		if ys.formatter == nil {
			return fmt.Sprintf("🎉 %s님이 구독자 %s명을 달성했습니다!\n축하합니다! 🎊",
				change.MemberName,
				util.FormatKoreanNumber(int64(milestone)))
		}
		msg, err := ys.formatter.FormatMilestoneAchieved(
			context.WithoutCancel(ctx),
			change.MemberName,
			util.FormatKoreanNumber(int64(milestone)),
		)
		if err != nil {
			ys.logger.Warn("마일스톤 달성 메시지 포맷 오류", slog.Any("error", err))
			return ""
		}
		return msg
	}

	return ""
}

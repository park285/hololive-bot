package youtube

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/util"
)

// MilestoneMessageFormatter: 마일스톤 관련 메시지 포맷터 최소 계약.
//
// NOTE: adapter 패키지 이동(P9-1) 대비를 위해 구체 타입(*adapter.ResponseFormatter) 대신
// 인터페이스를 통해 의존한다.
type MilestoneMessageFormatter interface {
	FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error)
	FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error)
}

// Scheduler: YouTube 데이터 수집(통계, 영상 등) 작업을 주기적으로 실행하는 스케줄러
type schedulerImpl struct {
	youtube              Service
	holodex              *holodex.Service
	cache                cache.Client
	statsRepo            StatsSchedulerRepository
	membersData          domain.MemberDataProvider
	alarmService         domain.AlarmDispatchState
	irisClient           iris.Client
	formatter            MilestoneMessageFormatter
	logger               *slog.Logger
	ticker               *time.Ticker
	milestoneWatchTicker *time.Ticker
	stopCh               chan struct{}
	currentBatch         int
	batchMu              sync.Mutex
}

const (
	schedulerInterval         = 12 * time.Hour
	milestoneWatchInterval    = 1 * time.Hour // 마일스톤 직전 멤버 빠른 체크
	MilestoneThresholdRatio   = 0.95          // 95% 이상이면 마일스톤 직전으로 간주
	ApproachingThresholdRatio = 0.99          // 99% 이상이면 예고 알람 발송

	channelsPerBatch = 30 // 30 channels × 100 units = 3,000 units per batch
	batchesPerDay    = 2  // 2 batches × 3,000 = 6,000 units
	totalDailyQuota  = 6000
)

// SubscriberMilestones: 구독자 수 마일스톤 목록 (중복 정의 방지)
var SubscriberMilestones = []uint64{
	100000, 250000, 500000, 750000, 1000000,
	1500000, 2000000, 2500000, 3000000, 4000000, 5000000,
}

// NewScheduler: YouTube 데이터 수집 스케줄러를 생성합니다.
func NewScheduler(
	youtubeSvc Service,
	holodexSvc *holodex.Service,
	cacheSvc cache.Client,
	statsRepo StatsSchedulerRepository,
	membersData domain.MemberDataProvider,
	alarmSvc domain.AlarmDispatchState,
	irisClient iris.Client,
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

// Start: 스케줄러를 시작하여 주기적인 작업을 등록합니다.
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

// Stop: 스케줄러를 중지합니다.
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
	ys.batchMu.Lock()
	batchNum := ys.currentBatch
	ys.currentBatch = (ys.currentBatch + 1) % batchesPerDay
	ys.batchMu.Unlock()

	ys.logger.Info("Running YouTube quota building batch",
		slog.Int("batch", batchNum),
		slog.Int("total_batches", batchesPerDay))

	go ys.trackAllSubscribers(ctx)

	go ys.fetchRecentVideosRotation(ctx, batchNum)
}

func (ys *schedulerImpl) trackAllSubscribers(ctx context.Context) {
	channelIDs, channelToMember := ys.buildChannelMaps()

	ys.logger.Info("Tracking all member subscribers",
		slog.Int("channels", len(channelIDs)),
		slog.Int("quota_cost", len(channelIDs)))

	stats, err := ys.youtube.GetChannelStatistics(ctx, channelIDs)
	fetchErrors := 0
	if err != nil {
		fetchErrors = len(channelIDs)
		stats = make(map[string]*ChannelStats, 0)
	}

	now := time.Now()
	totalChanges := 0
	totalMilestones := 0
	totalRecordErrors := 0
	totalSaveErrors := 0
	totalGetLatestErrors := 0
	totalMilestoneCheckErrors := 0
	totalMilestoneSaveErrors := 0

	type channelStatsWorkItem struct {
		channelID        string
		member           *domain.Member
		currentStats     *ChannelStats
		prevStats        *domain.TimestampedStats
		timestampedStats *domain.TimestampedStats
	}

	workItems := make([]channelStatsWorkItem, 0, len(stats))
	statsBatch := make([]*domain.TimestampedStats, 0, len(stats))

	for channelID, currentStats := range stats {
		member := channelToMember[channelID]
		if member == nil {
			continue
		}

		prevStats, latestErr := ys.statsRepo.GetLatestStats(ctx, channelID)
		if latestErr != nil {
			totalGetLatestErrors++
		}

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

	if len(statsBatch) > 0 {
		if err := ys.statsRepo.SaveStatsBatch(ctx, statsBatch); err != nil {
			totalSaveErrors = len(statsBatch)
			ys.logger.Warn("Failed to batch save subscriber stats",
				slog.Int("count", len(statsBatch)),
				slog.Any("error", err))
		} else {
			for _, item := range workItems {
				r := ys.processChannelStats(ctx, item.channelID, item.member, item.currentStats, item.prevStats, item.timestampedStats, now)
				totalChanges += r.changesDetected
				totalMilestones += r.milestonesAchieved
				totalRecordErrors += r.recordErrors
				totalMilestoneCheckErrors += r.milestoneCheckErrors
				totalMilestoneSaveErrors += r.milestoneSaveErrors
			}
		}
	}

	ys.logger.Info("Subscriber tracking completed",
		slog.Int("tracked", len(stats)),
		slog.Int("changes", totalChanges),
		slog.Int("milestones", totalMilestones),
		slog.Int("record_errors", totalRecordErrors),
		slog.Int("save_errors", totalSaveErrors),
		slog.Int("latest_stats_errors", totalGetLatestErrors),
		slog.Int("milestone_check_errors", totalMilestoneCheckErrors),
		slog.Int("milestone_save_errors", totalMilestoneSaveErrors),
		slog.Int("fetch_errors", fetchErrors))
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

// 달성된 마일스톤들을 저장하고 달성 개수 반환 (이미 달성한 마일스톤은 건너뜀)
func (ys *schedulerImpl) processMilestones(ctx context.Context, channelID string, member *domain.Member, milestones []uint64, now time.Time) (achieved int, checkErrors int, saveErrors int) {
	achieved = 0
	for _, milestone := range milestones {
		// 이미 달성한 마일스톤인지 확인 (재달성 방지)
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
	recordErrors         int
	milestoneCheckErrors int
	milestoneSaveErrors  int
}

// 단일 채널의 통계 처리 (저장, 변경 기록, 마일스톤)
func (ys *schedulerImpl) processChannelStats(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	currentStats *ChannelStats,
	prevStats *domain.TimestampedStats,
	timestampedStats *domain.TimestampedStats,
	now time.Time,
) channelStatsResult {
	var r channelStatsResult

	if prevStats != nil {
		subChange, vidChange, viewChange := calculateStatsChanges(prevStats, currentStats)

		if subChange != 0 || vidChange != 0 {
			change := &domain.StatsChange{
				ChannelID:        channelID,
				MemberName:       member.Name,
				SubscriberChange: subChange,
				VideoChange:      vidChange,
				ViewChange:       viewChange,
				PreviousStats:    prevStats,
				CurrentStats:     timestampedStats,
				DetectedAt:       now,
			}

			if err := ys.statsRepo.RecordChange(ctx, change); err != nil {
				r.recordErrors++
			} else {
				r.changesDetected = 1
			}

			milestones := ys.checkMilestones(prevStats.SubscriberCount, currentStats.SubscriberCount)
			r.milestonesAchieved, r.milestoneCheckErrors, r.milestoneSaveErrors = ys.processMilestones(ctx, channelID, member, milestones, now)
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

	for _, channelID := range channels {
		videos, err := ys.youtube.GetRecentVideos(ctx, channelID, 10)
		if err != nil {
			errorCount++
			continue
		}

		cacheKey := "youtube:recent_videos:" + channelID
		_ = ys.cache.Set(ctx, cacheKey, videos, 24*time.Hour)

		successCount++
	}

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
	var achieved []uint64
	for _, milestone := range SubscriberMilestones {
		if previous < milestone && current >= milestone {
			achieved = append(achieved, milestone)
		}
	}

	return achieved
}

// dispatchMilestoneAlerts: 알람이 설정된 방에 마일스톤 알람을 발송한다.
func (ys *schedulerImpl) dispatchMilestoneAlerts(ctx context.Context) {
	if ys.alarmService == nil || ys.irisClient == nil {
		return
	}

	// 알람이 설정된 고유 방 목록 조회
	rooms, err := ys.alarmService.GetDistinctRooms(ctx)
	if err != nil {
		ys.logger.Warn("Failed to get alarm rooms for milestone dispatch", slog.Any("error", err))
		return
	}

	if len(rooms) == 0 {
		return
	}

	// 메시지 발송 함수
	sendMessage := func(room, message string) error {
		return ys.irisClient.SendMessage(ctx, room, message)
	}

	if err := ys.SendMilestoneAlerts(ctx, sendMessage, rooms); err != nil {
		ys.logger.Warn("Failed to dispatch milestone alerts", slog.Any("error", err))
	}
}

// SendMilestoneAlerts: 감지된 중요 통계 변화(마일스톤 등)에 대해 채팅방에 알림 메시지를 전송합니다.
// 예고 알람(99% 도달)과 달성 알람 모두 처리한다.
func (ys *schedulerImpl) SendMilestoneAlerts(ctx context.Context, sendMessage func(room, message string) error, rooms []string) error {
	// 1. 예고 알람 처리 (99% 도달)
	approachingSent := ys.sendApproachingAlerts(ctx, sendMessage, rooms)

	// 2. 마일스톤 달성 알람 처리 (youtube_milestones 테이블에서 직접 조회)
	milestones, err := ys.statsRepo.GetUnnotifiedMilestones(ctx, 50)
	if err != nil {
		ys.logger.Warn("Failed to get unnotified milestones", slog.Any("error", err))
	}

	milestoneSent := 0
	for _, m := range milestones {
		var msg string
		if ys.formatter == nil {
			msg = fmt.Sprintf("🎉 %s님이 구독자 %s명을 달성했습니다!\n축하합니다! 🎊",
				m.MemberName,
				util.FormatKoreanNumber(int64(m.Value)))
		} else {
			formatted, err := ys.formatter.FormatMilestoneAchieved(ctx, m.MemberName, util.FormatKoreanNumber(int64(m.Value)))
			if err != nil {
				ys.logger.Warn("마일스톤 달성 메시지 포맷 오류", slog.Any("error", err))
				continue
			}
			msg = formatted
		}

		for _, room := range rooms {
			if err := sendMessage(room, msg); err != nil {
				ys.logger.Error("Failed to send milestone notification",
					slog.String("room", room),
					slog.String("member", m.MemberName),
					slog.Any("error", err))
				continue
			}
		}

		if err := ys.statsRepo.MarkMilestoneNotified(ctx, m.ChannelID, m.Type, m.Value); err != nil {
			ys.logger.Warn("Failed to mark milestone notified",
				slog.String("channel", m.ChannelID),
				slog.Any("error", err))
		} else {
			milestoneSent++
		}
	}

	totalSent := milestoneSent + approachingSent
	if totalSent > 0 {
		ys.logger.Info("Milestone notifications sent",
			slog.Int("achievements", milestoneSent),
			slog.Int("approaching", approachingSent))
	}

	return nil
}

// sendApproachingAlerts: 예고 알람(99% 도달)을 채팅방에 발송한다.
func (ys *schedulerImpl) sendApproachingAlerts(ctx context.Context, sendMessage func(room, message string) error, rooms []string) int {
	notifications, err := ys.statsRepo.GetUnnotifiedApproaching(ctx, 50)
	if err != nil {
		ys.logger.Warn("Failed to get unnotified approaching alerts", slog.Any("error", err))
		return 0
	}

	if len(notifications) == 0 {
		return 0
	}

	sentCount := 0
	for _, n := range notifications {
		message := ys.formatApproachingMessage(ctx, n.MemberName, n.MilestoneValue, n.CurrentSubs)

		for _, room := range rooms {
			if err := sendMessage(room, message); err != nil {
				ys.logger.Error("Failed to send approaching notification",
					slog.String("room", room),
					slog.String("member", n.MemberName),
					slog.Any("error", err))
				continue
			}
		}

		if err := ys.statsRepo.MarkApproachingChatNotified(ctx, n.ChannelID, n.MilestoneValue); err != nil {
			ys.logger.Warn("Failed to mark approaching notified",
				slog.String("channel", n.ChannelID),
				slog.Any("error", err))
		} else {
			sentCount++
		}
	}

	return sentCount
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
		if ctx == nil {
			ctx = context.Background()
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

// watchNearMilestoneMembers: Holodex API를 사용하여 마일스톤 직전 멤버를 빠르게 체크한다.
// 95% 이상 진행된 멤버만 체크하여 API 호출을 최소화한다.
func (ys *schedulerImpl) watchNearMilestoneMembers(ctx context.Context) {
	// 모든 채널의 마일스톤 직전 여부를 한 번에 조회 (threshold: 95%)
	nearMembers, err := ys.statsRepo.GetNearMilestoneMembers(ctx, MilestoneThresholdRatio, SubscriberMilestones, 50)
	if err != nil {
		ys.logger.Error("Failed to get near milestone members", slog.Any("error", err))
		return
	}

	if len(nearMembers) == 0 {
		return
	}

	// channelID -> Member 맵 구성
	_, channelToMember := ys.buildChannelMaps()
	channelMap := ys.getNearMilestoneChannelMap(ctx, nearMembers, channelToMember)

	ys.logger.Info("Checking near-milestone members via Holodex",
		slog.Int("count", len(nearMembers)))

	now := time.Now()
	for _, nm := range nearMembers {
		// Member 객체 조회
		member := channelToMember[nm.ChannelID]
		if member == nil {
			continue
		}

		// Holodex 배치 조회 결과 사용, 누락 시 개별 조회로 폴백
		channel := channelMap[nm.ChannelID]
		if channel == nil {
			channel, err = ys.holodex.GetChannel(ctx, nm.ChannelID)
			if err != nil {
				ys.logger.Warn("Failed to get channel from Holodex",
					slog.String("channel", nm.ChannelID),
					slog.Any("error", err))
				continue
			}
		}
		if channel == nil || channel.SubscriberCount == nil {
			continue
		}

		currentSubs := uint64(*channel.SubscriberCount)
		prevSubs := nm.CurrentSubs // DB에서 조회한 이전 구독자 수

		// 마일스톤 달성 여부 확인
		milestones := ys.checkMilestones(prevSubs, currentSubs)
		if len(milestones) > 0 {
			achieved, _, _ := ys.processMilestones(ctx, nm.ChannelID, member, milestones, now)
			if achieved > 0 {
				ys.logger.Info("Milestone detected via Holodex watcher",
					slog.String("member", member.Name),
					slog.Any("milestones", milestones),
					slog.Any("current_subs", currentSubs))

				// 통계 저장 (Holodex 데이터로 업데이트)
				stats := &domain.TimestampedStats{
					ChannelID:       nm.ChannelID,
					MemberName:      member.Name,
					SubscriberCount: currentSubs,
					Timestamp:       now,
				}
				if err := ys.statsRepo.SaveStats(ctx, stats); err != nil {
					ys.logger.Warn("Failed to save Holodex stats",
						slog.String("channel", nm.ChannelID),
						slog.Any("error", err))
				}
			}
		} else {
			// 마일스톤 미달성 상태에서 99% 이상 도달 시 예고 알람 체크
			ys.checkApproachingAlert(ctx, nm, member, currentSubs, now)
		}
	}
}

func (ys *schedulerImpl) getNearMilestoneChannelMap(
	ctx context.Context,
	nearMembers []NearMilestoneEntry,
	channelToMember map[string]*domain.Member,
) map[string]*domain.Channel {
	channelIDs := make([]string, 0, len(nearMembers))
	for _, nm := range nearMembers {
		if channelToMember[nm.ChannelID] == nil {
			continue
		}
		channelIDs = append(channelIDs, nm.ChannelID)
	}

	if len(channelIDs) == 0 {
		return make(map[string]*domain.Channel)
	}

	channelMap, err := ys.holodex.GetChannels(ctx, channelIDs)
	if err != nil {
		ys.logger.Warn("Failed to batch fetch near-milestone channels; using per-channel fallback",
			slog.Int("count", len(channelIDs)),
			slog.Any("error", err),
		)
		return make(map[string]*domain.Channel)
	}

	ys.logger.Debug("Near-milestone channel batch fetched",
		slog.Int("requested", len(channelIDs)),
		slog.Int("fetched", len(channelMap)),
	)
	return channelMap
}

// checkApproachingAlert: 99% 이상 도달 시 예고 알람을 발송한다 (중복 방지)
func (ys *schedulerImpl) checkApproachingAlert(ctx context.Context, nm NearMilestoneEntry, member *domain.Member, currentSubs uint64, now time.Time) {
	// 현재 진행률 계산 (최신 구독자 수 기준)
	progressPct := float64(currentSubs) / float64(nm.NextMilestone)
	if progressPct < ApproachingThresholdRatio {
		return // 99% 미만 → 예고 알람 대상 아님
	}

	// 이미 예고 알람을 발송했는지 확인
	alreadyNotified, err := ys.statsRepo.HasApproachingNotified(ctx, nm.ChannelID, nm.NextMilestone)
	if err != nil {
		ys.logger.Warn("Failed to check approaching notification status",
			slog.String("channel", nm.ChannelID),
			slog.Any("error", err))
		return
	}
	if alreadyNotified {
		return // 이미 예고 알람 발송 완료
	}

	// 예고 알람 기록 저장 (중복 방지)
	if err := ys.statsRepo.SaveApproachingNotification(ctx, nm.ChannelID, nm.NextMilestone, currentSubs, now); err != nil {
		ys.logger.Warn("Failed to save approaching notification",
			slog.String("channel", nm.ChannelID),
			slog.Any("error", err))
		return
	}

	remaining := nm.NextMilestone - currentSubs
	ys.logger.Info("Approaching milestone alert triggered",
		slog.String("member", member.Name),
		slog.Any("milestone", nm.NextMilestone),
		slog.Any("current_subs", currentSubs),
		slog.Any("remaining", remaining))
}

// formatApproachingMessage: 마일스톤 예고 알람 메시지를 생성합니다.
func (ys *schedulerImpl) formatApproachingMessage(ctx context.Context, memberName string, milestone, currentSubs uint64) string {
	remaining := milestone - currentSubs
	if ys.formatter == nil {
		return fmt.Sprintf("📍 %s님이 구독자 %s명까지 %s명 남았습니다!\n곧 마일스톤 달성이 예상됩니다! 🎯",
			memberName,
			util.FormatKoreanNumber(int64(milestone)),
			util.FormatKoreanNumber(int64(remaining)))
	}

	msg, err := ys.formatter.FormatMilestoneApproaching(
		ctx,
		memberName,
		util.FormatKoreanNumber(int64(milestone)),
		util.FormatKoreanNumber(int64(remaining)),
	)
	if err != nil {
		// 폴백: 하드코딩 메시지 (템플릿 실패 시)
		return fmt.Sprintf("📍 %s님이 구독자 %s명까지 %s명 남았습니다!\n곧 마일스톤 달성이 예상됩니다! 🎯",
			memberName,
			util.FormatKoreanNumber(int64(milestone)),
			util.FormatKoreanNumber(int64(remaining)))
	}
	return msg
}

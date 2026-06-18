package milestonescheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

func calculateStatsChanges(prev *domain.TimestampedStats, current *ChannelStats) (subChange, vidChange, viewChange int64) {
	subChange = uint64DeltaToInt64(current.SubscriberCount, prev.SubscriberCount)
	vidChange = uint64DeltaToInt64(current.VideoCount, prev.VideoCount)
	viewChange = uint64DeltaToInt64(current.ViewCount, prev.ViewCount)
	return
}

func uint64DeltaToInt64(current, previous uint64) int64 {
	if current >= previous {
		delta := current - previous
		if delta > math.MaxInt64 {
			return math.MaxInt64
		}
		return int64(delta)
	}
	delta := previous - current
	if delta > math.MaxInt64 {
		return math.MinInt64
	}
	return -int64(delta)
}

func formatMilestoneNumber(value uint64) string {
	if value > math.MaxInt64 {
		return util.FormatKoreanNumber(math.MaxInt64)
	}
	return util.FormatKoreanNumber(int64(value))
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

func (ys *schedulerImpl) processMilestones(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	milestones []uint64,
	achievedMilestones []uint64,
	milestonePreloadAvailable bool,
	now time.Time,
) (achieved, checkErrors, saveErrors int) {
	achievedSet := buildMilestoneSet(achievedMilestones)
	for _, milestone := range milestones {
		alreadyAchieved, checkErr := ys.milestoneAlreadyAchieved(ctx, channelID, member, milestone, achievedSet, milestonePreloadAvailable)
		if checkErr {
			checkErrors++
			continue
		}
		if alreadyAchieved {
			continue
		}
		if ys.saveSubscriberMilestone(ctx, channelID, member, milestone, now) {
			achieved++
		} else {
			saveErrors++
		}
	}
	return achieved, checkErrors, saveErrors
}

func (ys *schedulerImpl) milestoneAlreadyAchieved(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	milestone uint64,
	achievedSet map[uint64]struct{},
	preloadAvailable bool,
) (alreadyAchieved, checkErr bool) {
	if preloadAvailable {
		_, exists := achievedSet[milestone]
		if exists {
			ys.logMilestoneAlreadyAchieved(member, milestone)
		}
		return exists, false
	}
	alreadyAchieved, err := ys.statsRepository.HasAchievedMilestone(ctx, channelID, domain.MilestoneSubscribers, milestone)
	if err != nil {
		return false, true
	}
	if alreadyAchieved {
		ys.logMilestoneAlreadyAchieved(member, milestone)
	}
	return alreadyAchieved, false
}

func (ys *schedulerImpl) logMilestoneAlreadyAchieved(member *domain.Member, milestone uint64) {
	ys.logger.Debug("Milestone already achieved, skipping",
		slog.String("member", member.Name),
		slog.Any("value", milestone))
}

func (ys *schedulerImpl) saveSubscriberMilestone(
	ctx context.Context,
	channelID string,
	member *domain.Member,
	milestone uint64,
	now time.Time,
) bool {
	milestoneRecord := &domain.Milestone{
		ChannelID:  channelID,
		MemberName: member.Name,
		Type:       domain.MilestoneSubscribers,
		Value:      milestone,
		AchievedAt: now,
		Notified:   false,
	}
	if err := ys.statsRepository.SaveMilestone(ctx, milestoneRecord); err != nil {
		return false
	}
	ys.logger.Info("Milestone achieved",
		slog.String("member", member.Name),
		slog.Any("subscribers", milestone))
	return true
}

func (ys *schedulerImpl) checkMilestones(previous, current uint64) []uint64 {
	achieved := make([]uint64, 0, len(SubscriberMilestones))
	for _, milestone := range SubscriberMilestones {
		if previous < milestone && current >= milestone {
			achieved = append(achieved, milestone)
		}
	}

	return achieved
}

func (ys *schedulerImpl) isSignificantChange(change *domain.StatsChange) bool {
	if change.PreviousStats != nil && change.CurrentStats != nil {
		milestones := ys.checkMilestones(change.PreviousStats.SubscriberCount, change.CurrentStats.SubscriberCount)
		if len(milestones) > 0 {
			return true
		}
	}

	return false
}

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
				formatMilestoneNumber(milestone))
		}
		msg, err := ys.formatter.FormatMilestoneAchieved(
			context.WithoutCancel(ctx),
			change.MemberName,
			formatMilestoneNumber(milestone),
		)
		if err != nil {
			ys.logger.Warn("마일스톤 달성 메시지 포맷 오류", slog.Any("error", err))
			return ""
		}
		return msg
	}

	return ""
}

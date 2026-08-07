package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

const tieringQueryTimeout = 10 * time.Second

type SchedulerSyncer struct {
	scheduler     *scheduler.Scheduler
	registrations []providers.ChannelPollerRegistration
	tieringDB     *pgxpool.Pool
	logger        *slog.Logger
}

func (s *SchedulerSyncer) SyncAt(ctx context.Context, targets Targets, now time.Time) {
	if s == nil || s.scheduler == nil {
		return
	}
	tieredTargets, hasTieredTargets := s.classifyTargetsForTieredRegistrations(ctx, targets, now)
	tieredSyncs := make(map[string][]scheduler.PollerTargetSync)
	for i := range s.registrations {
		s.syncRegistration(tieredSyncs, &s.registrations[i], targets, &tieredTargets, hasTieredTargets)
	}
	for _, syncs := range tieredSyncs {
		s.scheduler.SyncPollerTargetGroups(syncs)
	}
}

func (s *SchedulerSyncer) syncRegistration(
	tieredSyncs map[string][]scheduler.PollerTargetSync,
	registration *providers.ChannelPollerRegistration,
	targets Targets,
	tieredTargets *TieredTargets,
	hasTieredTargets bool,
) {
	if !shouldSyncYouTubePollRegistration(registration) {
		return
	}
	if isTieredNotificationTargetGroup(registration.TargetGroup) {
		// 분류 실패 시 boot 시점 registration.ChannelIDs로 fallback-sync하면
		// desired set에 없는 현재 채널의 job이 삭제된다 — sync를 건너뛰어
		// 스케줄러의 마지막 정상 상태를 유지한다.
		if !hasTieredTargets {
			return
		}
		sync := youtubePollRegistrationTargetSync(registration, targets, tieredTargets, hasTieredTargets)
		tieredSyncs[registration.Poller.Name()] = append(tieredSyncs[registration.Poller.Name()], sync)
		return
	}
	sync := youtubePollRegistrationTargetSync(registration, targets, tieredTargets, hasTieredTargets)
	s.scheduler.SyncPollerTargets(&sync)
}

func (s *SchedulerSyncer) classifyTargetsForTieredRegistrations(ctx context.Context, targets Targets, now time.Time) (TieredTargets, bool) {
	if !s.hasTieredRegistrations() {
		return TieredTargets{}, false
	}
	if err := ctx.Err(); err != nil {
		s.logTieredClassifySkipped(err)
		return TieredTargets{}, false
	}
	classifyCtx, cancel := context.WithTimeout(ctx, tieringQueryTimeout)
	defer cancel()
	tieredTargets, err := classifyYouTubePollTargetsByActivity(classifyCtx, s.tieringDB, targets, now)
	if err != nil {
		s.logTieredClassifySkipped(err)
		return TieredTargets{}, false
	}
	return tieredTargets, true
}

func (s *SchedulerSyncer) logTieredClassifySkipped(err error) {
	if s.logger != nil {
		s.logger.Warn("youtube_poll_target_tiered_classify_skipped", slog.Any("error", err))
	}
}

func (s *SchedulerSyncer) hasTieredRegistrations() bool {
	return s != nil && hasTieredNotificationRegistration(s.registrations)
}

func shouldSyncYouTubePollRegistration(registration *providers.ChannelPollerRegistration) bool {
	return registration != nil &&
		registration.Poller != nil &&
		registration.Interval > 0 &&
		registration.HasExplicitChannelIDs
}

func youtubePollRegistrationTargetSync(
	registration *providers.ChannelPollerRegistration,
	targets Targets,
	tieredTargets *TieredTargets,
	hasTieredTargets bool,
) scheduler.PollerTargetSync {
	if registration == nil {
		return scheduler.PollerTargetSync{}
	}
	updated := cloneYouTubePollRegistration(registration)
	updated.ChannelIDs = youtubePollRegistrationChannelIDs(registration, targets, tieredTargets, hasTieredTargets)

	sync := updated.ToTargetSync()
	if isNotificationTargetGroup(registration.TargetGroup) {
		sync.ForceImmediateFirstRun = true
	}
	return sync
}

func cloneYouTubePollRegistration(registration *providers.ChannelPollerRegistration) providers.ChannelPollerRegistration {
	if registration == nil {
		return providers.ChannelPollerRegistration{}
	}
	clone := *registration
	if registration.ChannelPollerRegistrationOptions != nil {
		options := *registration.ChannelPollerRegistrationOptions
		options.ChannelIDs = append([]string(nil), registration.ChannelIDs...)
		clone.ChannelPollerRegistrationOptions = &options
	}
	return clone
}

func youtubePollRegistrationChannelIDs(
	registration *providers.ChannelPollerRegistration,
	targets Targets,
	tieredTargets *TieredTargets,
	hasTieredTargets bool,
) []string {
	if registration.TargetGroup == providers.ChannelTargetGroupStats {
		return append([]string(nil), targets.StatsChannelIDs...)
	}
	if registration.TargetGroup == providers.ChannelTargetGroupGlobal {
		return append([]string(nil), registration.ChannelIDs...)
	}
	if isTieredNotificationTargetGroup(registration.TargetGroup) {
		return tieredRegistrationChannelIDs(registration, tieredTargets, hasTieredTargets)
	}
	if registration.TargetGroup == providers.ChannelTargetGroupNotification {
		return append([]string(nil), targets.NotificationChannelIDs...)
	}
	return append([]string(nil), registration.ChannelIDs...)
}

func tieredRegistrationChannelIDs(
	registration *providers.ChannelPollerRegistration,
	targets *TieredTargets,
	hasTargets bool,
) []string {
	if !hasTargets || targets == nil {
		return append([]string(nil), registration.ChannelIDs...)
	}
	return append([]string(nil), channelIDsForTierGroup(registration.TargetGroup, targets)...)
}

func channelIDsForTierGroup(group providers.ChannelTargetGroup, targets *TieredTargets) []string {
	if targets == nil {
		return nil
	}
	if group == providers.ChannelTargetGroupActive {
		return targets.ActiveNotificationChannelIDs
	}
	if group == providers.ChannelTargetGroupWarm {
		return targets.WarmNotificationChannelIDs
	}
	if group == providers.ChannelTargetGroupCold {
		return targets.ColdNotificationChannelIDs
	}
	return nil
}

func hasTieredNotificationRegistration(registrations []providers.ChannelPollerRegistration) bool {
	for i := range registrations {
		if isTieredNotificationTargetGroup(registrations[i].TargetGroup) {
			return true
		}
	}
	return false
}

func isNotificationTargetGroup(group providers.ChannelTargetGroup) bool {
	return group == providers.ChannelTargetGroupNotification || isTieredNotificationTargetGroup(group)
}

func isTieredNotificationTargetGroup(group providers.ChannelTargetGroup) bool {
	return group == providers.ChannelTargetGroupActive ||
		group == providers.ChannelTargetGroupWarm ||
		group == providers.ChannelTargetGroupCold
}

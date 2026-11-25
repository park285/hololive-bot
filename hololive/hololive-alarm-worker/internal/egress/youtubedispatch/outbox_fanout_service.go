package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type OutboxFanoutService struct {
	transition *store.TransitionStore
	grouper    *OutboxGrouper
	logger     *slog.Logger
	config     Config
}

func newOutboxFanoutService(
	transition *store.TransitionStore,
	grouper *OutboxGrouper,
	logger *slog.Logger,
	config *Config,
) *OutboxFanoutService {
	if transition == nil {
		return nil
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &OutboxFanoutService{
		transition: transition, grouper: grouper, logger: logger, config: *config,
	}
}

func (s *OutboxFanoutService) Claim(ctx context.Context) ([]domain.YouTubeNotificationOutbox, error) {
	items, err := s.transition.ClaimOutboxesForFanout(ctx, s.config.BatchSize)
	if err != nil {
		return nil, fmt.Errorf("claim outbox fanout batch: %w", err)
	}

	return items, nil
}

func (s *OutboxFanoutService) materialize(
	ctx context.Context,
	items []domain.YouTubeNotificationOutbox,
) outboxEnqueueStats {
	startedAt := time.Now()

	defer func() {
		outboxMetrics().enqueueDuration.Observe(time.Since(startedAt).Seconds())
	}()

	roomsByChannel := s.grouper.collectRoomsByChannel(ctx, items)
	stats := outboxEnqueueStats{}

	for i := range items {
		stats.add(s.materializeOne(ctx, &items[i], roomsByChannel))
	}

	return stats
}

func (s *OutboxFanoutService) materializeOne(
	ctx context.Context,
	item *domain.YouTubeNotificationOutbox,
	roomsByChannel map[string]channelAlarmRoomTargets,
) outboxEnqueueStats {
	rooms, ok := roomsForItem(roomsByChannel, item)
	if !ok {
		return s.fanoutLookupFailure(ctx, item, "subscriber lookup failed")
	}

	rooms = s.grouper.filterLiveCatchupSuppressedRooms(ctx, item, rooms)

	roomIDs, pausedRooms, err := s.withoutPausedRooms(ctx, deliveryRoomIDs(rooms))
	if err != nil {
		s.logger.Warn("Failed to load paused kakao rooms for outbox fanout", slog.Int64("outbox_id", item.ID), slog.Any("error", err))

		return s.fanoutLookupFailure(ctx, item, "paused room lookup failed")
	}

	result, err := s.transition.MaterializeFanout(ctx, *item, roomIDs)
	observeFanoutResult(result, err)

	if err != nil || result.Outcome != store.ApplyApplied {
		s.logger.Error("Failed to materialize canonical outbox fanout",
			slog.Int64("outbox_id", item.ID),
			slog.String("outcome", result.Outcome.String()),
			slog.Int("target_room_count", len(roomIDs)),
			slog.Any("error", err))

		return outboxEnqueueStats{enqueueFailures: 1}
	}

	if result.NoTargets {
		return outboxEnqueueStats{noSubscriberOutboxes: 1, pausedRooms: pausedRooms}
	}

	return outboxEnqueueStats{enqueuedOutboxes: 1, totalTargetRooms: result.DeliveryCount, pausedRooms: pausedRooms}
}

func (s *OutboxFanoutService) withoutPausedRooms(ctx context.Context, roomIDs []string) ([]string, int, error) {
	paused, err := s.transition.PausedRooms(ctx, roomIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("filter paused rooms: %w", err)
	}

	if len(paused) == 0 {
		return roomIDs, 0, nil
	}

	active := make([]string, 0, len(roomIDs)-len(paused))
	for _, roomID := range roomIDs {
		if !paused[roomID] {
			active = append(active, roomID)
		}
	}

	return active, len(roomIDs) - len(active), nil
}

func (s *OutboxFanoutService) fanoutLookupFailure(ctx context.Context, item *domain.YouTubeNotificationOutbox, reason string) outboxEnqueueStats {
	applied, err := s.transition.ApplyFanoutFailure(ctx, *item, reason)
	observeLifecycleApply("fanout_failure", applied, err, 1)

	if err != nil || applied.Outcome != store.ApplyApplied {
		s.logger.Error("Failed to persist canonical outbox fanout failure",
			slog.Int64("outbox_id", item.ID),
			slog.String("outcome", applied.Outcome.String()),
			slog.Any("error", err))
	}

	return outboxEnqueueStats{subscriberLookupFailures: 1}
}

package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type OutboxFanoutService struct {
	transition *store.TransitionStore
	grouper    *OutboxGrouper
	logger     *slog.Logger
	config     dispatchstate.Config
}

func newOutboxFanoutService(
	transition *store.TransitionStore,
	grouper *OutboxGrouper,
	logger *slog.Logger,
	config *dispatchstate.Config,
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
		outboxEnqueueDuration.Observe(time.Since(startedAt).Seconds())
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
		applied, err := s.transition.ApplyFanoutFailure(ctx, *item, "subscriber lookup failed")
		observeLifecycleApply("fanout_failure", applied, err, 1)

		if err != nil || applied.Outcome != store.ApplyApplied {
			s.logger.Error("Failed to persist canonical outbox fanout failure",
				slog.Int64("outbox_id", item.ID),
				slog.String("outcome", applied.Outcome.String()),
				slog.Any("error", err))
		}

		return outboxEnqueueStats{subscriberLookupFailures: 1}
	}

	rooms = s.grouper.filterLiveCatchupSuppressedRooms(ctx, item, rooms)

	roomIDs := deliveryRoomIDs(rooms)
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
		return outboxEnqueueStats{noSubscriberOutboxes: 1}
	}

	return outboxEnqueueStats{enqueuedOutboxes: 1, totalTargetRooms: result.DeliveryCount}
}

package communityshortsops

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func normalizeObservationAlarmSentHistoryInputs(
	ctx context.Context,
	logger *slog.Logger,
	now time.Time,
) (context.Context, *slog.Logger, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}
	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return ctx, logger, now
}

func findObservationAlarmSentHistoryWindow(
	ctx context.Context,
	session *communityShortsOpsSession,
	query observationAlarmSentHistoryQuery,
	now time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	window, err := session.trackingRepository.FindClosedCommunityShortsObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("find observation window: %w", err)
	}
	if window == nil {
		return nil, fmt.Errorf(
			"observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return window, nil
}

func buildObservationAlarmSentHistoryComparisonForWindow(
	ctx context.Context,
	session *communityShortsOpsSession,
	query observationAlarmSentHistoryQuery,
	window *domain.YouTubeCommunityShortsObservationWindow,
	outboxKind domain.OutboxKind,
) (trackingrepo.ObservationPostComparisonResult, error) {
	return buildObservationAlarmSentHistoryComparison(
		ctx,
		session.trackingRepository,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		outboxKind,
	)
}

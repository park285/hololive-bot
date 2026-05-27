package alarmhistory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func normalizeCollectInputs(
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
	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return ctx, logger, now
}

func findObservationWindow(
	ctx context.Context,
	session *shared.OpsSession,
	query variantQuery,
	now time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	window, err := session.TrackingRepository.FindClosedCommunityShortsObservationWindow(
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
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return window, nil
}

func buildComparisonForWindow(
	ctx context.Context,
	session *shared.OpsSession,
	query variantQuery,
	window *domain.YouTubeCommunityShortsObservationWindow,
	outboxKind domain.OutboxKind,
) (trackingrepo.ObservationPostComparisonResult, error) {
	return buildObservationAlarmSentHistoryComparison(
		ctx,
		session.TrackingRepository,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		outboxKind,
	)
}

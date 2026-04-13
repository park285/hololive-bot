package ops

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type CommunityAlarmSentHistoryCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityAlarmSentHistoryQuery = observationAlarmSentHistoryQuery
type CommunityAlarmSentHistorySummary = observationAlarmSentHistorySummary

type CommunityAlarmSentHistoryReport struct {
	GeneratedAt time.Time                                    `json:"generated_at"`
	Query       CommunityAlarmSentHistoryQuery               `json:"query"`
	Summary     CommunityAlarmSentHistorySummary             `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult `json:"comparison"`
	Rows        []trackingrepo.CommunityAlarmSentHistoryRow  `json:"rows"`
}

func CollectCommunityAlarmSentHistoryReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityAlarmSentHistoryCollectOptions,
) (CommunityAlarmSentHistoryReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeObservationAlarmSentHistoryCollectQuery(options.ObservationRuntimeName, options.ObservationBigBangCutoverAt)
	if err != nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	trackingRepository := trackingrepo.NewRepository(db)
	window, err := trackingRepository.FindClosedCommunityShortsObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: find observation window: %w", err)
	}
	if window == nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf(
			"collect community alarm sent history report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	query.WindowStart = cloneCommunityShortsSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&window.ObservationEndedAt)

	rows, err := trackingRepository.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: list sent histories: %w", err)
	}

	comparison, err := buildObservationAlarmSentHistoryComparison(
		ctx,
		trackingRepository,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		domain.OutboxKindCommunityPost,
	)
	if err != nil {
		return CommunityAlarmSentHistoryReport{}, fmt.Errorf("collect community alarm sent history report: build comparison: %w", err)
	}

	return BuildCommunityAlarmSentHistoryReport(rows, comparison, query, now), nil
}

func BuildCommunityAlarmSentHistoryReport(
	rows []trackingrepo.CommunityAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query CommunityAlarmSentHistoryQuery,
	generatedAt time.Time,
) CommunityAlarmSentHistoryReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeObservationAlarmSentHistoryQuery(query)

	finalized := finalizeCommunityAlarmSentHistoryRows(rows)
	summary := buildObservationAlarmSentHistorySummary(finalized)

	return CommunityAlarmSentHistoryReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Comparison:  comparison,
		Rows:        finalized.Rows,
	}
}

func RenderCommunityAlarmSentHistoryMarkdown(report CommunityAlarmSentHistoryReport) string {
	return renderObservationAlarmSentHistoryMarkdown(
		"YouTube Community Alarm Sent History",
		report.GeneratedAt,
		report.Query,
		report.Summary,
		report.Comparison,
		report.Rows,
		"관찰 구간에 해당하는 발송 완료 community 알람 이력이 없습니다.",
	)
}

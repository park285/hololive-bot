package ops

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type ShortsAlarmSentHistoryCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type ShortsAlarmSentHistoryQuery = observationAlarmSentHistoryQuery
type ShortsAlarmSentHistorySummary = observationAlarmSentHistorySummary

type ShortsAlarmSentHistoryReport struct {
	GeneratedAt time.Time                                    `json:"generated_at"`
	Query       ShortsAlarmSentHistoryQuery                  `json:"query"`
	Summary     ShortsAlarmSentHistorySummary                `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult `json:"comparison"`
	Rows        []trackingrepo.ShortsAlarmSentHistoryRow     `json:"rows"`
}

func CollectShortsAlarmSentHistoryReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options ShortsAlarmSentHistoryCollectOptions,
) (ShortsAlarmSentHistoryReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: config is nil")
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
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: %w", err)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	if session == nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: session is nil")
	}

	window, err := session.trackingRepository.FindClosedCommunityShortsObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: find observation window: %w", err)
	}
	if window == nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf(
			"collect shorts alarm sent history report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	query.WindowStart = cloneCommunityShortsSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&window.ObservationEndedAt)

	rows, err := session.trackingRepository.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(
		ctx,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
	)
	if err != nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: list sent histories: %w", err)
	}

	comparison, err := buildObservationAlarmSentHistoryComparison(
		ctx,
		session.trackingRepository,
		query.ObservationRuntimeName,
		window.BigBangCutoverAt,
		window.ObservationStartedAt,
		window.ObservationEndedAt,
		domain.OutboxKindNewShort,
	)
	if err != nil {
		return ShortsAlarmSentHistoryReport{}, fmt.Errorf("collect shorts alarm sent history report: build comparison: %w", err)
	}

	return BuildShortsAlarmSentHistoryReport(rows, comparison, query, now), nil
}

func BuildShortsAlarmSentHistoryReport(
	rows []trackingrepo.ShortsAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query ShortsAlarmSentHistoryQuery,
	generatedAt time.Time,
) ShortsAlarmSentHistoryReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeObservationAlarmSentHistoryQuery(query)

	finalized := finalizeShortsAlarmSentHistoryRows(rows)
	summary := buildObservationAlarmSentHistorySummary(finalized)

	return ShortsAlarmSentHistoryReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Comparison:  comparison,
		Rows:        finalized.Rows,
	}
}

func RenderShortsAlarmSentHistoryMarkdown(report ShortsAlarmSentHistoryReport) string {
	return renderObservationAlarmSentHistoryMarkdown(
		"YouTube Shorts Alarm Sent History",
		report.GeneratedAt,
		report.Query,
		report.Summary,
		report.Comparison,
		report.Rows,
		"관찰 구간에 해당하는 발송 완료 shorts 알람 이력이 없습니다.",
	)
}

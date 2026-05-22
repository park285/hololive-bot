package reports

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func CollectShortsAlarmSentHistoryReport(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options ShortsAlarmSentHistoryCollectOptions,
) (ShortsAlarmSentHistoryReport, error) {
	return collectObservationAlarmSentHistoryWithDefinition(
		ctx,
		appConfig,
		logger,
		now,
		options,
		shortsObservationAlarmSentHistoryDefinition,
		BuildShortsAlarmSentHistoryReport,
	)
}

func BuildShortsAlarmSentHistoryReport(
	rows []trackingrepo.ShortsAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query ShortsAlarmSentHistoryQuery,
	generatedAt time.Time,
) ShortsAlarmSentHistoryReport {
	return buildObservationAlarmSentHistoryWithDefinition(
		rows,
		comparison,
		query,
		generatedAt,
		shortsObservationAlarmSentHistoryDefinition,
		func(
			generatedAt time.Time,
			query observationAlarmSentHistoryQuery,
			summary observationAlarmSentHistorySummary,
			comparison trackingrepo.ObservationPostComparisonResult,
			rows []trackingrepo.ObservationAlarmSentHistoryRow,
		) ShortsAlarmSentHistoryReport {
			return ShortsAlarmSentHistoryReport{
				GeneratedAt: generatedAt,
				Query:       query,
				Summary:     summary,
				Comparison:  comparison,
				Rows:        rows,
			}
		},
	)
}

func RenderShortsAlarmSentHistoryMarkdown(report ShortsAlarmSentHistoryReport) string {
	return renderObservationAlarmSentHistoryWithDefinition(report, shortsObservationAlarmSentHistoryDefinition)
}

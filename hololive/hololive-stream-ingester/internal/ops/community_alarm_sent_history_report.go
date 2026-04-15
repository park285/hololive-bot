package ops

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func CollectCommunityAlarmSentHistoryReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityAlarmSentHistoryCollectOptions,
) (CommunityAlarmSentHistoryReport, error) {
	return collectObservationAlarmSentHistoryWithDefinition(
		ctx,
		cfg,
		logger,
		now,
		options,
		communityObservationAlarmSentHistoryDefinition,
		BuildCommunityAlarmSentHistoryReport,
	)
}

func BuildCommunityAlarmSentHistoryReport(
	rows []trackingrepo.CommunityAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query CommunityAlarmSentHistoryQuery,
	generatedAt time.Time,
) CommunityAlarmSentHistoryReport {
	return buildObservationAlarmSentHistoryWithDefinition(
		rows,
		comparison,
		query,
		generatedAt,
		communityObservationAlarmSentHistoryDefinition,
		func(
			generatedAt time.Time,
			query observationAlarmSentHistoryQuery,
			summary observationAlarmSentHistorySummary,
			comparison trackingrepo.ObservationPostComparisonResult,
			rows []trackingrepo.ObservationAlarmSentHistoryRow,
		) CommunityAlarmSentHistoryReport {
			return CommunityAlarmSentHistoryReport{
				GeneratedAt: generatedAt,
				Query:       query,
				Summary:     summary,
				Comparison:  comparison,
				Rows:        rows,
			}
		},
	)
}

func RenderCommunityAlarmSentHistoryMarkdown(report CommunityAlarmSentHistoryReport) string {
	return renderObservationAlarmSentHistoryWithDefinition(report, communityObservationAlarmSentHistoryDefinition)
}

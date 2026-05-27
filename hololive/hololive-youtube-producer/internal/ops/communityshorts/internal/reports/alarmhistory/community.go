package alarmhistory

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func CollectCommunity(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityCollectOptions,
) (CommunityReport, error) {
	return collectWithDefinition(
		ctx,
		appConfig,
		logger,
		now,
		options,
		communityDefinition,
		func(rows []trackingrepo.ObservationAlarmSentHistoryRow, comparison trackingrepo.ObservationPostComparisonResult, query variantQuery, generatedAt time.Time) CommunityReport {
			return BuildCommunity(rows, comparison, CommunityQuery(query), generatedAt)
		},
	)
}

func BuildCommunity(
	rows []trackingrepo.CommunityAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query CommunityQuery,
	generatedAt time.Time,
) CommunityReport {
	return buildWithDefinition(
		rows,
		comparison,
		variantQuery(query),
		generatedAt,
		communityDefinition,
		func(
			generatedAt time.Time,
			query variantQuery,
			summary variantSummary,
			comparison trackingrepo.ObservationPostComparisonResult,
			rows []trackingrepo.ObservationAlarmSentHistoryRow,
		) CommunityReport {
			return CommunityReport{
				GeneratedAt: generatedAt,
				Query:       CommunityQuery(query),
				Summary:     CommunitySummary(summary),
				Comparison:  comparison,
				Rows:        rows,
			}
		},
	)
}

func RenderCommunityMarkdown(report CommunityReport) string {
	return renderVariantMarkdown(variantReport{
		GeneratedAt: report.GeneratedAt,
		Query:       variantQuery(report.Query),
		Summary:     variantSummary(report.Summary),
		Comparison:  report.Comparison,
		Rows:        report.Rows,
	}, communityDefinition)
}

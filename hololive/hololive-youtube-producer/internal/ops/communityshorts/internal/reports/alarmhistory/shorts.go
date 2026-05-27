package alarmhistory

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func CollectShorts(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options ShortsCollectOptions,
) (ShortsReport, error) {
	return collectWithDefinition(
		ctx,
		appConfig,
		logger,
		now,
		CommunityCollectOptions{
			ObservationRuntimeName:      options.ObservationRuntimeName,
			ObservationBigBangCutoverAt: options.ObservationBigBangCutoverAt,
		},
		shortsDefinition,
		func(rows []trackingrepo.ObservationAlarmSentHistoryRow, comparison trackingrepo.ObservationPostComparisonResult, query variantQuery, generatedAt time.Time) ShortsReport {
			return BuildShorts(rows, comparison, ShortsQuery(query), generatedAt)
		},
	)
}

func BuildShorts(
	rows []trackingrepo.ShortsAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query ShortsQuery,
	generatedAt time.Time,
) ShortsReport {
	return buildWithDefinition(
		rows,
		comparison,
		variantQuery(query),
		generatedAt,
		shortsDefinition,
		func(
			generatedAt time.Time,
			query variantQuery,
			summary variantSummary,
			comparison trackingrepo.ObservationPostComparisonResult,
			rows []trackingrepo.ObservationAlarmSentHistoryRow,
		) ShortsReport {
			return ShortsReport{
				GeneratedAt: generatedAt,
				Query:       ShortsQuery(query),
				Summary:     ShortsSummary(summary),
				Comparison:  comparison,
				Rows:        rows,
			}
		},
	)
}

func RenderShortsMarkdown(report ShortsReport) string {
	return renderVariantMarkdown(variantReport{
		GeneratedAt: report.GeneratedAt,
		Query:       variantQuery(report.Query),
		Summary:     variantSummary(report.Summary),
		Comparison:  report.Comparison,
		Rows:        report.Rows,
	}, shortsDefinition)
}

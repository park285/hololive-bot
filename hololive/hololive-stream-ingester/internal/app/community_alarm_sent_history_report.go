package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

type CommunityAlarmSentHistoryQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type CommunityAlarmSentHistorySummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

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

	query, err := normalizeCommunityAlarmSentHistoryCollectOptions(options)
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
	query = normalizeCommunityAlarmSentHistoryQuery(query)

	finalized := finalizeCommunityAlarmSentHistoryRows(rows)
	summary := CommunityAlarmSentHistorySummary{
		CollectedRowCount: finalized.CollectedRowCount,
		DuplicateRowCount: finalized.DuplicateRowCount,
	}
	for i := range finalized.Rows {
		row := finalized.Rows[i]
		summary.SentPostCount++
		if summary.EarliestAlarmSentAt == nil || row.AlarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
			summary.EarliestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
		if summary.LatestAlarmSentAt == nil || row.AlarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
			summary.LatestAlarmSentAt = cloneCommunityShortsSendCountTime(&row.AlarmSentAt)
		}
	}

	return CommunityAlarmSentHistoryReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Comparison:  comparison,
		Rows:        finalized.Rows,
	}
}

func RenderCommunityAlarmSentHistoryMarkdown(report CommunityAlarmSentHistoryReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community Alarm Sent History\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- summary: collected_rows=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.CollectedRowCount))
	builder.WriteString("`, duplicates_removed=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.DuplicateRowCount))
	builder.WriteString("`, sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SentPostCount))
	builder.WriteString("`, earliest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.EarliestAlarmSentAt))
	builder.WriteString("`, latest_alarm_sent_at=`")
	builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Summary.LatestAlarmSentAt))
	builder.WriteString("`\n")
	builder.WriteString(renderObservationPostComparisonSummaryMarkdown(report.Comparison))
	builder.WriteString(renderObservationPostComparisonVerdictsMarkdown(report.Comparison))
	builder.WriteString(renderObservationIdentifierMismatchCandidatesMarkdown(report.Comparison))

	if len(report.Rows) == 0 {
		builder.WriteString("\n관찰 구간에 해당하는 발송 완료 community 알람 이력이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| post_id | channel_id | content_id | actual_published_at | detected_at | alarm_sent_at |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.PostID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ContentID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.AlarmSentAt))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func normalizeCommunityAlarmSentHistoryCollectOptions(options CommunityAlarmSentHistoryCollectOptions) (CommunityAlarmSentHistoryQuery, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return CommunityAlarmSentHistoryQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return CommunityAlarmSentHistoryQuery{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeCommunityAlarmSentHistoryQuery(query CommunityAlarmSentHistoryQuery) CommunityAlarmSentHistoryQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}

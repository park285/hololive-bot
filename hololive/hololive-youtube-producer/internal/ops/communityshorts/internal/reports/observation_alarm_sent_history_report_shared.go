package reports

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	md "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/markdown"
)

type observationAlarmSentHistoryCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type observationAlarmSentHistoryQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type observationAlarmSentHistorySummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type observationAlarmSentHistoryReport struct {
	GeneratedAt time.Time                                     `json:"generated_at"`
	Query       observationAlarmSentHistoryQuery              `json:"query"`
	Summary     observationAlarmSentHistorySummary            `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult  `json:"comparison"`
	Rows        []trackingrepo.ObservationAlarmSentHistoryRow `json:"rows"`
}

type CommunityAlarmSentHistoryCollectOptions = observationAlarmSentHistoryCollectOptions
type CommunityAlarmSentHistoryQuery = observationAlarmSentHistoryQuery
type CommunityAlarmSentHistorySummary = observationAlarmSentHistorySummary
type CommunityAlarmSentHistoryReport = observationAlarmSentHistoryReport

type ShortsAlarmSentHistoryCollectOptions = observationAlarmSentHistoryCollectOptions
type ShortsAlarmSentHistoryQuery = observationAlarmSentHistoryQuery
type ShortsAlarmSentHistorySummary = observationAlarmSentHistorySummary
type ShortsAlarmSentHistoryReport = observationAlarmSentHistoryReport

type observationAlarmSentHistoryDefinition struct {
	reportName   string
	title        string
	emptyMessage string
	outboxKind   domain.OutboxKind
	listRows     func(context.Context, *communityShortsOpsSession, string, time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error)
	finalizeRows func([]trackingrepo.ObservationAlarmSentHistoryRow) observationAlarmSentHistoryFinalizationResult
}

var communityObservationAlarmSentHistoryDefinition = observationAlarmSentHistoryDefinition{
	reportName:   "community alarm sent history report",
	title:        "YouTube Community Alarm Sent History",
	emptyMessage: "관찰 구간에 해당하는 발송 완료 community 알람 이력이 없습니다.",
	outboxKind:   domain.OutboxKindCommunityPost,
	listRows: func(ctx context.Context, session *communityShortsOpsSession, runtimeName string, cutoverAt time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
		return session.trackingRepository.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, cutoverAt)
	},
	finalizeRows: finalizeCommunityAlarmSentHistoryRows,
}

var shortsObservationAlarmSentHistoryDefinition = observationAlarmSentHistoryDefinition{
	reportName:   "shorts alarm sent history report",
	title:        "YouTube Shorts Alarm Sent History",
	emptyMessage: "관찰 구간에 해당하는 발송 완료 shorts 알람 이력이 없습니다.",
	outboxKind:   domain.OutboxKindNewShort,
	listRows: func(ctx context.Context, session *communityShortsOpsSession, runtimeName string, cutoverAt time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
		return session.trackingRepository.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, cutoverAt)
	},
	finalizeRows: finalizeShortsAlarmSentHistoryRows,
}

func buildObservationAlarmSentHistorySummary(finalized observationAlarmSentHistoryFinalizationResult) observationAlarmSentHistorySummary {
	summary := observationAlarmSentHistorySummary{
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
	return summary
}

func collectObservationAlarmSentHistoryWithDefinition[Report any](
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options observationAlarmSentHistoryCollectOptions,
	definition observationAlarmSentHistoryDefinition,
	buildReport func([]trackingrepo.ObservationAlarmSentHistoryRow, trackingrepo.ObservationPostComparisonResult, observationAlarmSentHistoryQuery, time.Time) Report,
) (Report, error) {
	return collectObservationAlarmSentHistoryReport(
		ctx,
		appConfig,
		logger,
		now,
		options,
		definition.reportName,
		definition.outboxKind,
		definition.listRows,
		buildReport,
	)
}

func collectObservationAlarmSentHistoryReport[Report any](
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options observationAlarmSentHistoryCollectOptions,
	reportName string,
	outboxKind domain.OutboxKind,
	listRows func(context.Context, *communityShortsOpsSession, string, time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error),
	buildReport func([]trackingrepo.ObservationAlarmSentHistoryRow, trackingrepo.ObservationPostComparisonResult, observationAlarmSentHistoryQuery, time.Time) Report,
) (Report, error) {
	var zero Report

	ctx, logger, now = normalizeObservationAlarmSentHistoryInputs(ctx, logger, now)

	query, err := normalizeObservationAlarmSentHistoryCollectQuery(options.ObservationRuntimeName, options.ObservationBigBangCutoverAt)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", reportName, err)
	}
	if appConfig == nil {
		return zero, fmt.Errorf("collect %s: config is nil", reportName)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, appConfig, logger)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", reportName, err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}
	if session == nil {
		return zero, fmt.Errorf("collect %s: session is nil", reportName)
	}

	window, err := findObservationAlarmSentHistoryWindow(ctx, session, query, now)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", reportName, err)
	}
	query.WindowStart = cloneCommunityShortsSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&window.ObservationEndedAt)

	rows, err := listRows(ctx, session, query.ObservationRuntimeName, window.BigBangCutoverAt)
	if err != nil {
		return zero, fmt.Errorf("collect %s: list sent histories: %w", reportName, err)
	}

	comparison, err := buildObservationAlarmSentHistoryComparisonForWindow(
		ctx,
		session,
		query,
		window,
		outboxKind,
	)
	if err != nil {
		return zero, fmt.Errorf("collect %s: build comparison: %w", reportName, err)
	}

	return buildReport(rows, comparison, query, now), nil
}

func buildObservationAlarmSentHistoryWithDefinition[Report any](
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query observationAlarmSentHistoryQuery,
	generatedAt time.Time,
	definition observationAlarmSentHistoryDefinition,
	buildReport func(time.Time, observationAlarmSentHistoryQuery, observationAlarmSentHistorySummary, trackingrepo.ObservationPostComparisonResult, []trackingrepo.ObservationAlarmSentHistoryRow) Report,
) Report {
	return buildObservationAlarmSentHistoryReport(
		rows,
		comparison,
		query,
		generatedAt,
		definition.finalizeRows,
		buildReport,
	)
}

func buildObservationAlarmSentHistoryReport[Report any](
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	comparison trackingrepo.ObservationPostComparisonResult,
	query observationAlarmSentHistoryQuery,
	generatedAt time.Time,
	finalizeRows func([]trackingrepo.ObservationAlarmSentHistoryRow) observationAlarmSentHistoryFinalizationResult,
	buildReport func(time.Time, observationAlarmSentHistoryQuery, observationAlarmSentHistorySummary, trackingrepo.ObservationPostComparisonResult, []trackingrepo.ObservationAlarmSentHistoryRow) Report,
) Report {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	query = normalizeObservationAlarmSentHistoryQuery(query)
	finalized := finalizeRows(rows)

	return buildReport(
		generatedAt,
		query,
		buildObservationAlarmSentHistorySummary(finalized),
		comparison,
		finalized.Rows,
	)
}

func renderObservationAlarmSentHistoryWithDefinition(report observationAlarmSentHistoryReport, definition observationAlarmSentHistoryDefinition) string {
	return renderObservationAlarmSentHistoryMarkdown(
		definition.title,
		report.GeneratedAt,
		report.Query,
		report.Summary,
		report.Comparison,
		report.Rows,
		definition.emptyMessage,
	)
}

func renderObservationAlarmSentHistoryMarkdown(
	title string,
	generatedAt time.Time,
	query observationAlarmSentHistoryQuery,
	summary observationAlarmSentHistorySummary,
	comparison trackingrepo.ObservationPostComparisonResult,
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	emptyMessage string,
) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, title)
	writeObservationAlarmSentHistoryMetadata(&builder, generatedAt, query, summary)
	builder.WriteString(renderObservationPostComparisonSummaryMarkdown(comparison))
	builder.WriteString(renderObservationPostComparisonVerdictsMarkdown(comparison))
	builder.WriteString(renderObservationIdentifierMismatchCandidatesMarkdown(comparison))

	if len(rows) == 0 {
		builder.WriteString("\n")
		builder.WriteString(emptyMessage)
		builder.WriteString("\n")
		return builder.String()
	}

	md.WriteTable(&builder, observationAlarmSentHistoryMarkdownColumns(), buildObservationAlarmSentHistoryMarkdownRows(rows))

	return builder.String()
}

func writeObservationAlarmSentHistoryMetadata(
	builder *strings.Builder,
	generatedAt time.Time,
	query observationAlarmSentHistoryQuery,
	summary observationAlarmSentHistorySummary,
) {
	md.WriteKV(builder, "generated at", md.Code(formatCommunityShortsSendCountTime(generatedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(fallbackCommunityShortsSendCountValue(query.ObservationRuntimeName))+
			", cutover: "+md.Code(formatCommunityShortsSendCountTimePtr(query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(formatCommunityShortsSendCountTimePtr(query.WindowStart))+
			" -> "+md.Code(formatCommunityShortsSendCountTimePtr(query.WindowEnd)),
	)
	md.WriteKV(builder, "summary", buildObservationAlarmSentHistorySummaryMarkdown(summary))
}

func buildObservationAlarmSentHistorySummaryMarkdown(summary observationAlarmSentHistorySummary) string {
	return "collected_rows=" + md.Code(fmt.Sprintf("%d", summary.CollectedRowCount)) +
		", duplicates_removed=" + md.Code(fmt.Sprintf("%d", summary.DuplicateRowCount)) +
		", sent_posts=" + md.Code(fmt.Sprintf("%d", summary.SentPostCount)) +
		", earliest_alarm_sent_at=" + md.Code(formatCommunityShortsSendCountTimePtr(summary.EarliestAlarmSentAt)) +
		", latest_alarm_sent_at=" + md.Code(formatCommunityShortsSendCountTimePtr(summary.LatestAlarmSentAt))
}

func observationAlarmSentHistoryMarkdownColumns() []md.Column {
	return []md.Column{
		{Header: "post_id"},
		{Header: "channel_id"},
		{Header: "content_id"},
		{Header: "actual_published_at"},
		{Header: "detected_at"},
		{Header: "alarm_sent_at"},
	}
}

func buildObservationAlarmSentHistoryMarkdownRows(rows []trackingrepo.ObservationAlarmSentHistoryRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(fallbackCommunityShortsSendCountValue(row.PostID)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ChannelID)),
			md.Code(fallbackCommunityShortsSendCountValue(row.ContentID)),
			md.Code(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(formatCommunityShortsSendCountTime(row.DetectedAt)),
			md.Code(formatCommunityShortsSendCountTime(row.AlarmSentAt)),
		})
	}
	return markdownRows
}

func normalizeObservationAlarmSentHistoryCollectQuery(
	runtimeName string,
	cutoverAt *time.Time,
) (observationAlarmSentHistoryQuery, error) {
	trimmedRuntimeName := strings.TrimSpace(runtimeName)
	normalizedCutoverAt := cloneCommunityShortsSendCountTime(cutoverAt)
	if trimmedRuntimeName == "" || normalizedCutoverAt == nil || normalizedCutoverAt.IsZero() {
		return observationAlarmSentHistoryQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}
	return observationAlarmSentHistoryQuery{
		ObservationRuntimeName:      trimmedRuntimeName,
		ObservationBigBangCutoverAt: normalizedCutoverAt,
	}, nil
}

func normalizeObservationAlarmSentHistoryQuery(query observationAlarmSentHistoryQuery) observationAlarmSentHistoryQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	return query
}

package alarmhistory

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
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type variantQuery struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
}

type variantSummary struct {
	CollectedRowCount   int        `json:"collected_row_count"`
	DuplicateRowCount   int        `json:"duplicate_row_count"`
	SentPostCount       int        `json:"sent_post_count"`
	EarliestAlarmSentAt *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt   *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type variantReport struct {
	GeneratedAt time.Time                                     `json:"generated_at"`
	Query       variantQuery                                  `json:"query"`
	Summary     variantSummary                                `json:"summary"`
	Comparison  trackingrepo.ObservationPostComparisonResult  `json:"comparison"`
	Rows        []trackingrepo.ObservationAlarmSentHistoryRow `json:"rows"`
}

type variantDefinition struct {
	reportName   string
	title        string
	emptyMessage string
	outboxKind   domain.OutboxKind
	listRows     func(context.Context, *shared.OpsSession, string, time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error)
	finalizeRows func([]trackingrepo.ObservationAlarmSentHistoryRow) finalizationResult
}

var communityDefinition = variantDefinition{
	reportName:   "community alarm sent history report",
	title:        "YouTube Community Alarm Sent History",
	emptyMessage: "관찰 구간에 해당하는 발송 완료 community 알람 이력이 없습니다.",
	outboxKind:   domain.OutboxKindCommunityPost,
	listRows: func(ctx context.Context, session *shared.OpsSession, runtimeName string, cutoverAt time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
		return session.TrackingRepository.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, cutoverAt)
	},
	finalizeRows: finalizeCommunityAlarmSentHistoryRows,
}

var shortsDefinition = variantDefinition{
	reportName:   "shorts alarm sent history report",
	title:        "YouTube Shorts Alarm Sent History",
	emptyMessage: "관찰 구간에 해당하는 발송 완료 shorts 알람 이력이 없습니다.",
	outboxKind:   domain.OutboxKindNewShort,
	listRows: func(ctx context.Context, session *shared.OpsSession, runtimeName string, cutoverAt time.Time) ([]trackingrepo.ObservationAlarmSentHistoryRow, error) {
		return session.TrackingRepository.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx, runtimeName, cutoverAt)
	},
	finalizeRows: finalizeShortsAlarmSentHistoryRows,
}

func buildVariantSummary(finalized finalizationResult) variantSummary {
	summary := variantSummary{
		CollectedRowCount: finalized.CollectedRowCount,
		DuplicateRowCount: finalized.DuplicateRowCount,
	}
	for i := range finalized.Rows {
		row := finalized.Rows[i]
		summary.SentPostCount++
		if summary.EarliestAlarmSentAt == nil || row.AlarmSentAt.Before(summary.EarliestAlarmSentAt.UTC()) {
			summary.EarliestAlarmSentAt = shared.CloneSendCountTime(&row.AlarmSentAt)
		}
		if summary.LatestAlarmSentAt == nil || row.AlarmSentAt.After(summary.LatestAlarmSentAt.UTC()) {
			summary.LatestAlarmSentAt = shared.CloneSendCountTime(&row.AlarmSentAt)
		}
	}
	return summary
}

func collectWithDefinition[Report any](
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityCollectOptions,
	definition *variantDefinition,
	buildReport func([]trackingrepo.ObservationAlarmSentHistoryRow, *trackingrepo.ObservationPostComparisonResult, variantQuery, time.Time) Report,
) (Report, error) {
	var zero Report

	ctx, logger, now = normalizeCollectInputs(ctx, logger, now)

	query, err := normalizeVariantCollectQuery(options.ObservationRuntimeName, options.ObservationBigBangCutoverAt)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", definition.reportName, err)
	}
	if appConfig == nil {
		return zero, fmt.Errorf("collect %s: config is nil", definition.reportName)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", definition.reportName, err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}
	if session == nil {
		return zero, fmt.Errorf("collect %s: session is nil", definition.reportName)
	}

	window, err := findObservationWindow(ctx, session, query, now)
	if err != nil {
		return zero, fmt.Errorf("collect %s: %w", definition.reportName, err)
	}
	query.WindowStart = shared.CloneSendCountTime(&window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&window.ObservationEndedAt)

	rows, err := definition.listRows(ctx, session, query.ObservationRuntimeName, window.BigBangCutoverAt)
	if err != nil {
		return zero, fmt.Errorf("collect %s: list sent histories: %w", definition.reportName, err)
	}

	comparison, err := buildComparisonForWindow(ctx, session, query, window, definition.outboxKind)
	if err != nil {
		return zero, fmt.Errorf("collect %s: build comparison: %w", definition.reportName, err)
	}

	return buildReport(rows, &comparison, query, now), nil
}

func buildWithDefinition[Report any](
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	comparison *trackingrepo.ObservationPostComparisonResult,
	query variantQuery,
	generatedAt time.Time,
	definition *variantDefinition,
	buildReport func(time.Time, variantQuery, variantSummary, *trackingrepo.ObservationPostComparisonResult, []trackingrepo.ObservationAlarmSentHistoryRow) Report,
) Report {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	query = normalizeVariantQuery(query)
	finalized := definition.finalizeRows(rows)

	return buildReport(
		generatedAt,
		query,
		buildVariantSummary(finalized),
		comparison,
		finalized.Rows,
	)
}

func renderVariantMarkdown(report *variantReport, definition *variantDefinition) string {
	return renderVariantAlarmSentHistoryMarkdown(
		definition.title,
		report.GeneratedAt,
		report.Query,
		report.Summary,
		&report.Comparison,
		report.Rows,
		definition.emptyMessage,
	)
}

func renderVariantAlarmSentHistoryMarkdown(
	title string,
	generatedAt time.Time,
	query variantQuery,
	summary variantSummary,
	comparison *trackingrepo.ObservationPostComparisonResult,
	rows []trackingrepo.ObservationAlarmSentHistoryRow,
	emptyMessage string,
) string {
	var builder strings.Builder

	md.WriteHeading(&builder, 1, title)
	writeVariantMetadata(&builder, generatedAt, query, summary)
	builder.WriteString(renderComparisonSummaryMarkdown(comparison))
	builder.WriteString(renderComparisonVerdictsMarkdown(comparison))
	builder.WriteString(renderIdentifierMismatchCandidatesMarkdown(comparison))

	if len(rows) == 0 {
		builder.WriteString("\n")
		builder.WriteString(emptyMessage)
		builder.WriteString("\n")
		return builder.String()
	}

	md.WriteTable(&builder, variantMarkdownColumns(), buildVariantMarkdownRows(rows))

	return builder.String()
}

func writeVariantMetadata(
	builder *strings.Builder,
	generatedAt time.Time,
	query variantQuery,
	summary variantSummary,
) {
	md.WriteKV(builder, "generated at", md.Code(shared.FormatSendCountTime(generatedAt)))
	md.WriteKV(
		builder,
		"observation runtime",
		md.Code(shared.FallbackSendCountValue(query.ObservationRuntimeName))+
			", cutover: "+md.Code(shared.FormatSendCountTimePtr(query.ObservationBigBangCutoverAt)),
	)
	md.WriteKV(
		builder,
		"window",
		md.Code(shared.FormatSendCountTimePtr(query.WindowStart))+
			" -> "+md.Code(shared.FormatSendCountTimePtr(query.WindowEnd)),
	)
	md.WriteKV(builder, "summary", buildVariantSummaryMarkdown(summary))
}

func buildVariantSummaryMarkdown(summary variantSummary) string {
	return "collected_rows=" + md.Code(fmt.Sprintf("%d", summary.CollectedRowCount)) +
		", duplicates_removed=" + md.Code(fmt.Sprintf("%d", summary.DuplicateRowCount)) +
		", sent_posts=" + md.Code(fmt.Sprintf("%d", summary.SentPostCount)) +
		", earliest_alarm_sent_at=" + md.Code(shared.FormatSendCountTimePtr(summary.EarliestAlarmSentAt)) +
		", latest_alarm_sent_at=" + md.Code(shared.FormatSendCountTimePtr(summary.LatestAlarmSentAt))
}

func variantMarkdownColumns() []md.Column {
	return []md.Column{
		{Header: "post_id"},
		{Header: "channel_id"},
		{Header: "content_id"},
		{Header: "actual_published_at"},
		{Header: "detected_at"},
		{Header: "alarm_sent_at"},
	}
}

func buildVariantMarkdownRows(rows []trackingrepo.ObservationAlarmSentHistoryRow) [][]string {
	markdownRows := make([][]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		markdownRows = append(markdownRows, []string{
			md.Code(shared.FallbackSendCountValue(row.PostID)),
			md.Code(shared.FallbackSendCountValue(row.ChannelID)),
			md.Code(shared.FallbackSendCountValue(row.ContentID)),
			md.Code(shared.FormatSendCountTimePtr(row.ActualPublishedAt)),
			md.Code(shared.FormatSendCountTime(row.DetectedAt)),
			md.Code(shared.FormatSendCountTime(row.AlarmSentAt)),
		})
	}
	return markdownRows
}

func normalizeVariantCollectQuery(
	runtimeName string,
	cutoverAt *time.Time,
) (variantQuery, error) {
	trimmedRuntimeName := strings.TrimSpace(runtimeName)
	normalizedCutoverAt := shared.CloneSendCountTime(cutoverAt)
	if trimmedRuntimeName == "" || normalizedCutoverAt == nil || normalizedCutoverAt.IsZero() {
		return variantQuery{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}
	return variantQuery{
		ObservationRuntimeName:      trimmedRuntimeName,
		ObservationBigBangCutoverAt: normalizedCutoverAt,
	}, nil
}

func normalizeVariantQuery(query variantQuery) variantQuery {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	return query
}

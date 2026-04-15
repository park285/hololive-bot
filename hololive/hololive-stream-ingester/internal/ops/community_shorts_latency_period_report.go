package ops

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

type CommunityShortsLatencyPeriodSpec struct {
	Label  string        `json:"label"`
	Window time.Duration `json:"window"`
}

type CommunityShortsLatencyPeriodReport struct {
	GeneratedAt time.Time                         `json:"generated_at"`
	Periods     []outbox.PostLatencyPeriodSummary `json:"periods"`
}

func DefaultCommunityShortsLatencyPeriodSpecs() []CommunityShortsLatencyPeriodSpec {
	return []CommunityShortsLatencyPeriodSpec{
		{Label: "last_1h", Window: time.Hour},
		{Label: "last_24h", Window: 24 * time.Hour},
		{Label: "last_7d", Window: 7 * 24 * time.Hour},
	}
}

func CollectCommunityShortsLatencyPeriodReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	specs []CommunityShortsLatencyPeriodSpec,
) (CommunityShortsLatencyPeriodReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsLatencyPeriodReport{}, fmt.Errorf("collect community shorts latency period report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	periods, err := buildCommunityShortsLatencyPeriods(now, specs)
	if err != nil {
		return CommunityShortsLatencyPeriodReport{}, fmt.Errorf("collect community shorts latency period report: %w", err)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsLatencyPeriodReport{}, fmt.Errorf("collect community shorts latency period report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectCommunityShortsLatencyPeriodReportWithSession(ctx, session, now, periods)
}

func collectCommunityShortsLatencyPeriodReportWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyPeriodReport, error) {
	if session == nil {
		return CommunityShortsLatencyPeriodReport{}, fmt.Errorf("collect community shorts latency period report: session is nil")
	}

	summaries, err := session.telemetryRepo.ListPostLatencyPeriodSummaries(ctx, periods)
	if err != nil {
		return CommunityShortsLatencyPeriodReport{}, fmt.Errorf("collect community shorts latency period report: list period summaries: %w", err)
	}

	return BuildCommunityShortsLatencyPeriodReport(summaries, now), nil
}

func BuildCommunityShortsLatencyPeriodReport(
	rows []outbox.PostLatencyPeriodSummary,
	generatedAt time.Time,
) CommunityShortsLatencyPeriodReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedRows := make([]outbox.PostLatencyPeriodSummary, 0, len(rows))
	for i := range rows {
		normalizedRows = append(normalizedRows, cloneCommunityShortsLatencyPeriodSummary(rows[i]))
	}
	if len(normalizedRows) > 1 {
		sort.SliceStable(normalizedRows, func(i, j int) bool {
			if !normalizedRows[i].StartAt.Equal(normalizedRows[j].StartAt) {
				return normalizedRows[i].StartAt.Before(normalizedRows[j].StartAt)
			}
			return normalizedRows[i].Label < normalizedRows[j].Label
		})
	}

	return CommunityShortsLatencyPeriodReport{
		GeneratedAt: generatedAt,
		Periods:     normalizedRows,
	}
}

func RenderCommunityShortsLatencyPeriodMarkdown(report CommunityShortsLatencyPeriodReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Latency Period Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- periods: `")
	fmt.Fprintf(&builder, "%d", len(report.Periods))
	builder.WriteString("`\n")

	if len(report.Periods) == 0 {
		builder.WriteString("\n조회된 community/shorts 지연 기간 집계가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| period | window_start | window_end | total_posts | alarm_sent_posts | pending_posts | measured_posts | avg_latency_ms | p95_latency_ms | max_latency_ms | over_2m_posts | community_over_2m_posts | shorts_over_2m_posts |\n")
	builder.WriteString("| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for i := range report.Periods {
		row := report.Periods[i]
		builder.WriteString("| `")
		builder.WriteString(strings.TrimSpace(row.Label))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.StartAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.EndAt))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.TotalPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.AlarmSentPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.PendingPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.LatencyMeasuredPostCount)
		builder.WriteString(" | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.AverageLatencyMillis))
		builder.WriteString(" | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.P95LatencyMillis))
		builder.WriteString(" | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.MaxLatencyMillis))
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.ExceededPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.CommunityExceededPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.ShortsExceededPostCount)
		builder.WriteString(" |\n")
	}

	return builder.String()
}

func buildCommunityShortsLatencyPeriods(
	now time.Time,
	specs []CommunityShortsLatencyPeriodSpec,
) ([]outbox.PostLatencyPeriod, error) {
	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if len(specs) == 0 {
		specs = DefaultCommunityShortsLatencyPeriodSpecs()
	}

	periods := make([]outbox.PostLatencyPeriod, 0, len(specs))
	seenLabels := make(map[string]struct{}, len(specs))
	for i := range specs {
		label := strings.TrimSpace(specs[i].Label)
		if label == "" {
			return nil, fmt.Errorf("period spec at index %d: label is empty", i)
		}
		if specs[i].Window <= 0 {
			return nil, fmt.Errorf("period %q: window must be greater than zero", label)
		}
		if _, exists := seenLabels[label]; exists {
			return nil, fmt.Errorf("period %q: duplicate label", label)
		}
		seenLabels[label] = struct{}{}
		periods = append(periods, outbox.PostLatencyPeriod{
			Label:   label,
			StartAt: now.Add(-specs[i].Window),
			EndAt:   now,
		})
	}

	return periods, nil
}

func cloneCommunityShortsLatencyPeriodSummary(row outbox.PostLatencyPeriodSummary) outbox.PostLatencyPeriodSummary {
	row.Label = strings.TrimSpace(row.Label)
	row.StartAt = normalizeCommunityShortsSendCountTime(row.StartAt)
	row.EndAt = normalizeCommunityShortsSendCountTime(row.EndAt)
	row.AverageLatencyMillis = cloneCommunityShortsSendCountInt64(row.AverageLatencyMillis)
	row.P95LatencyMillis = cloneCommunityShortsSendCountInt64(row.P95LatencyMillis)
	row.MaxLatencyMillis = cloneCommunityShortsSendCountInt64(row.MaxLatencyMillis)
	return row
}

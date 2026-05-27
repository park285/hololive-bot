package latencycause

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type PeriodSpec struct {
	Label  string        `json:"label"`
	Window time.Duration `json:"window"`
}

type PeriodReport struct {
	GeneratedAt time.Time                         `json:"generated_at"`
	Periods     []outbox.PostLatencyPeriodSummary `json:"periods"`
}

func DefaultPeriodSpecs() []PeriodSpec {
	return []PeriodSpec{
		{Label: "last_1h", Window: time.Hour},
		{Label: "last_24h", Window: 24 * time.Hour},
		{Label: "last_7d", Window: 7 * 24 * time.Hour},
	}
}

func CollectPeriodReport(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	specs []PeriodSpec,
) (PeriodReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if appConfig == nil {
		return PeriodReport{}, fmt.Errorf("collect community shorts latency period report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	periods, err := buildPeriods(now, specs)
	if err != nil {
		return PeriodReport{}, fmt.Errorf("collect community shorts latency period report: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return PeriodReport{}, fmt.Errorf("collect community shorts latency period report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectPeriodReportWithSession(ctx, session, now, periods)
}

func collectPeriodReportWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	periods []outbox.PostLatencyPeriod,
) (PeriodReport, error) {
	if session == nil {
		return PeriodReport{}, fmt.Errorf("collect community shorts latency period report: session is nil")
	}

	summaries, err := session.TelemetryRepository.ListPostLatencyPeriodSummaries(ctx, periods)
	if err != nil {
		return PeriodReport{}, fmt.Errorf("collect community shorts latency period report: list period summaries: %w", err)
	}

	return BuildPeriodReport(summaries, now), nil
}

func BuildPeriodReport(
	rows []outbox.PostLatencyPeriodSummary,
	generatedAt time.Time,
) PeriodReport {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	normalizedRows := make([]outbox.PostLatencyPeriodSummary, 0, len(rows))
	for i := range rows {
		normalizedRows = append(normalizedRows, clonePeriodSummary(rows[i]))
	}
	if len(normalizedRows) > 1 {
		sort.SliceStable(normalizedRows, func(i, j int) bool {
			if !normalizedRows[i].StartAt.Equal(normalizedRows[j].StartAt) {
				return normalizedRows[i].StartAt.Before(normalizedRows[j].StartAt)
			}
			return normalizedRows[i].Label < normalizedRows[j].Label
		})
	}

	return PeriodReport{
		GeneratedAt: generatedAt,
		Periods:     normalizedRows,
	}
}

func RenderPeriodMarkdown(report PeriodReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Latency Period Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(shared.FormatSendCountTime(report.GeneratedAt))
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
		writePeriodRow(&builder, report.Periods[i])
	}

	return builder.String()
}

func writePeriodRow(builder *strings.Builder, row outbox.PostLatencyPeriodSummary) {
	builder.WriteString("| `")
	builder.WriteString(strings.TrimSpace(row.Label))
	builder.WriteString("` | `")
	builder.WriteString(shared.FormatSendCountTime(row.StartAt))
	builder.WriteString("` | `")
	builder.WriteString(shared.FormatSendCountTime(row.EndAt))
	builder.WriteString("` | ")
	fmt.Fprintf(builder, "%d", row.TotalPostCount)
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.AlarmSentPostCount)
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.PendingPostCount)
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.LatencyMeasuredPostCount)
	builder.WriteString(" | ")
	builder.WriteString(shared.FormatSendCountInt64Ptr(row.AverageLatencyMillis))
	builder.WriteString(" | ")
	builder.WriteString(shared.FormatSendCountInt64Ptr(row.P95LatencyMillis))
	builder.WriteString(" | ")
	builder.WriteString(shared.FormatSendCountInt64Ptr(row.MaxLatencyMillis))
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.ExceededPostCount)
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.CommunityExceededPostCount)
	builder.WriteString(" | ")
	fmt.Fprintf(builder, "%d", row.ShortsExceededPostCount)
	builder.WriteString(" |\n")
}

func buildPeriods(
	now time.Time,
	specs []PeriodSpec,
) ([]outbox.PostLatencyPeriod, error) {
	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if len(specs) == 0 {
		specs = DefaultPeriodSpecs()
	}

	periods := make([]outbox.PostLatencyPeriod, 0, len(specs))
	seenLabels := make(map[string]struct{}, len(specs))
	for i := range specs {
		label, err := validatePeriodSpec(i, specs[i], seenLabels)
		if err != nil {
			return nil, err
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

func validatePeriodSpec(
	index int,
	spec PeriodSpec,
	seenLabels map[string]struct{},
) (string, error) {
	label := strings.TrimSpace(spec.Label)
	if label == "" {
		return "", fmt.Errorf("period spec at index %d: label is empty", index)
	}
	if spec.Window <= 0 {
		return "", fmt.Errorf("period %q: window must be greater than zero", label)
	}
	if _, exists := seenLabels[label]; exists {
		return "", fmt.Errorf("period %q: duplicate label", label)
	}
	return label, nil
}

func clonePeriodSummary(row outbox.PostLatencyPeriodSummary) outbox.PostLatencyPeriodSummary {
	row.Label = strings.TrimSpace(row.Label)
	row.StartAt = shared.NormalizeSendCountTime(row.StartAt)
	row.EndAt = shared.NormalizeSendCountTime(row.EndAt)
	row.AverageLatencyMillis = shared.CloneSendCountInt64(row.AverageLatencyMillis)
	row.P95LatencyMillis = shared.CloneSendCountInt64(row.P95LatencyMillis)
	row.MaxLatencyMillis = shared.CloneSendCountInt64(row.MaxLatencyMillis)
	return row
}

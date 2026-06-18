package channelsummary

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

type Report struct {
	GeneratedAt time.Time                           `json:"generated_at"`
	WindowStart time.Time                           `json:"window_start"`
	WindowEnd   time.Time                           `json:"window_end"`
	Summary     Totals                              `json:"summary"`
	Rows        []outbox.ChannelPostDeliverySummary `json:"rows"`
}

type Totals struct {
	ChannelCount               int64 `json:"channel_count"`
	DetectedPostCount          int64 `json:"detected_post_count"`
	AlarmSentPostCount         int64 `json:"alarm_sent_post_count"`
	SuccessPostCount           int64 `json:"success_post_count"`
	FailedPostCount            int64 `json:"failed_post_count"`
	DetectedUnsentPostCount    int64 `json:"detected_unsent_post_count"`
	CommunityDetectedPostCount int64 `json:"community_detected_post_count"`
	ShortsDetectedPostCount    int64 `json:"shorts_detected_post_count"`
}

type request struct {
	logger *slog.Logger
	now    time.Time
	since  time.Time
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (Report, error) {
	req, err := normalizeRequest(ctx, appConfig, logger, now, since)
	if err != nil {
		return Report{}, err
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, req.logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts channel summary report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	if session == nil {
		return Report{}, fmt.Errorf("collect community shorts channel summary report: session is nil")
	}

	rows, err := session.TelemetryRepository.ListChannelPostDeliverySummariesSince(ctx, req.since)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts channel summary report: list channel summaries: %w", err)
	}

	return Build(rows, req.now, req.since), nil
}

func normalizeRequest(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (request, error) {
	if appConfig == nil {
		return request{}, fmt.Errorf("collect community shorts channel summary report: config is nil")
	}
	if ctx == nil {
		return request{}, fmt.Errorf("collect community shorts channel summary report: context is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	since = shared.NormalizeSendCountTime(since)
	if since.IsZero() {
		return request{}, fmt.Errorf("collect community shorts channel summary report: since is empty")
	}
	if since.After(now) {
		return request{}, fmt.Errorf("collect community shorts channel summary report: since is after now")
	}

	return request{
		logger: logger,
		now:    now,
		since:  since,
	}, nil
}

func Build(
	rows []outbox.ChannelPostDeliverySummary,
	generatedAt time.Time,
	since time.Time,
) Report {
	generatedAt = shared.NormalizeSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	since = shared.NormalizeSendCountTime(since)

	normalizedRows := make([]outbox.ChannelPostDeliverySummary, 0, len(rows))
	summary := Totals{}
	for i := range rows {
		row := rows[i]
		row.ChannelID = strings.TrimSpace(row.ChannelID)
		row.EarliestObservedAt = normalizeTimePtr(row.EarliestObservedAt)
		row.LatestObservedAt = normalizeTimePtr(row.LatestObservedAt)
		normalizedRows = append(normalizedRows, row)
		summary.ChannelCount++
		summary.DetectedPostCount += row.DetectedPostCount
		summary.AlarmSentPostCount += row.AlarmSentPostCount
		summary.SuccessPostCount += row.SuccessPostCount
		summary.FailedPostCount += row.FailedPostCount
		summary.DetectedUnsentPostCount += row.DetectedUnsentPostCount
		summary.CommunityDetectedPostCount += row.CommunityDetectedPostCount
		summary.ShortsDetectedPostCount += row.ShortsDetectedPostCount
	}

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		left := sortTime(&normalizedRows[i])
		right := sortTime(&normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return normalizedRows[i].ChannelID < normalizedRows[j].ChannelID
	})

	return Report{
		GeneratedAt: generatedAt,
		WindowStart: since,
		WindowEnd:   generatedAt,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderMarkdown(report *Report) string {
	if report == nil {
		return renderMarkdown(&Report{})
	}
	return renderMarkdown(report)
}

func renderMarkdown(report *Report) string {
	var builder strings.Builder

	builder.WriteString(renderMetadataMarkdown(report))

	if len(report.Rows) == 0 {
		builder.WriteString("\n최근 윈도우에 해당하는 community/shorts 감지 채널이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString(renderTableMarkdown(report.Rows))
	return builder.String()
}

func renderMetadataMarkdown(report *Report) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Channel Delivery Summary\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(shared.FormatSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(shared.FormatSendCountTime(report.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(shared.FormatSendCountTime(report.WindowEnd))
	builder.WriteString("`\n")
	builder.WriteString("- summary: channels=`")
	fmt.Fprintf(&builder, "%d", report.Summary.ChannelCount)
	builder.WriteString("`, detected_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.DetectedPostCount)
	builder.WriteString("`, alarm_sent_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.AlarmSentPostCount)
	builder.WriteString("`, success_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.SuccessPostCount)
	builder.WriteString("`, failed_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.FailedPostCount)
	builder.WriteString("`, detected_unsent_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.DetectedUnsentPostCount)
	builder.WriteString("`, community_detected_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.CommunityDetectedPostCount)
	builder.WriteString("`, shorts_detected_posts=`")
	fmt.Fprintf(&builder, "%d", report.Summary.ShortsDetectedPostCount)
	builder.WriteString("`\n")

	return builder.String()
}

func renderTableMarkdown(rows []outbox.ChannelPostDeliverySummary) string {
	var builder strings.Builder

	builder.WriteString("\n| status | channel_id | earliest_observed_at | latest_observed_at | detected_post_count | alarm_sent_post_count | success_post_count | failed_post_count | detected_unsent_post_count | community_detected_post_count | shorts_detected_post_count |\n")
	builder.WriteString("| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for i := range rows {
		row := rows[i]
		builder.WriteString("| `")
		builder.WriteString(resolveStatus(&row))
		builder.WriteString("` | `")
		builder.WriteString(shared.FallbackSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(shared.FormatSendCountTimePtr(row.EarliestObservedAt))
		builder.WriteString("` | `")
		builder.WriteString(shared.FormatSendCountTimePtr(row.LatestObservedAt))
		builder.WriteString("` | ")
		fmt.Fprintf(&builder, "%d", row.DetectedPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.AlarmSentPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.SuccessPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.FailedPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.DetectedUnsentPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.CommunityDetectedPostCount)
		builder.WriteString(" | ")
		fmt.Fprintf(&builder, "%d", row.ShortsDetectedPostCount)
		builder.WriteString(" |\n")
	}

	return builder.String()
}

func resolveStatus(row *outbox.ChannelPostDeliverySummary) string {
	if row == nil {
		return "ok"
	}
	return resolveStatusCounts(row.DetectedUnsentPostCount, row.FailedPostCount)
}

func resolveStatusCounts(detectedUnsentPostCount, failedPostCount int64) string {
	hasUnsent := detectedUnsentPostCount > 0
	hasFailures := failedPostCount > 0
	switch {
	case hasUnsent && hasFailures:
		return "unsent_with_failures"
	case hasUnsent:
		return "unsent_pending"
	case hasFailures:
		return "failures_observed"
	default:
		return "ok"
	}
}

func normalizeTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := shared.NormalizeSendCountTime(*value)
	return &normalized
}

func sortTime(row *outbox.ChannelPostDeliverySummary) time.Time {
	if row == nil {
		return time.Time{}
	}
	if row.LatestObservedAt != nil {
		return row.LatestObservedAt.UTC()
	}
	if row.EarliestObservedAt != nil {
		return row.EarliestObservedAt.UTC()
	}
	return time.Time{}
}

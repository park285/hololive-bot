package reports

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

type CommunityShortsChannelSummaryReport struct {
	GeneratedAt time.Time                           `json:"generated_at"`
	WindowStart time.Time                           `json:"window_start"`
	WindowEnd   time.Time                           `json:"window_end"`
	Summary     CommunityShortsChannelSummaryTotals `json:"summary"`
	Rows        []outbox.ChannelPostDeliverySummary `json:"rows"`
}

type CommunityShortsChannelSummaryTotals struct {
	ChannelCount               int64 `json:"channel_count"`
	DetectedPostCount          int64 `json:"detected_post_count"`
	AlarmSentPostCount         int64 `json:"alarm_sent_post_count"`
	SuccessPostCount           int64 `json:"success_post_count"`
	FailedPostCount            int64 `json:"failed_post_count"`
	DetectedUnsentPostCount    int64 `json:"detected_unsent_post_count"`
	CommunityDetectedPostCount int64 `json:"community_detected_post_count"`
	ShortsDetectedPostCount    int64 `json:"shorts_detected_post_count"`
}

func CollectCommunityShortsChannelSummaryReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (CommunityShortsChannelSummaryReport, error) {
	request, err := normalizeCommunityShortsChannelSummaryRequest(ctx, cfg, logger, now, since)
	if err != nil {
		return CommunityShortsChannelSummaryReport{}, err
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(request.ctx, cfg, request.logger)
	if err != nil {
		return CommunityShortsChannelSummaryReport{}, fmt.Errorf("collect community shorts channel summary report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	if session == nil {
		return CommunityShortsChannelSummaryReport{}, fmt.Errorf("collect community shorts channel summary report: session is nil")
	}

	rows, err := session.telemetryRepository.ListChannelPostDeliverySummariesSince(request.ctx, request.since)
	if err != nil {
		return CommunityShortsChannelSummaryReport{}, fmt.Errorf("collect community shorts channel summary report: list channel summaries: %w", err)
	}

	return BuildCommunityShortsChannelSummaryReport(rows, request.now, request.since), nil
}

type communityShortsChannelSummaryRequest struct {
	ctx    context.Context
	logger *slog.Logger
	now    time.Time
	since  time.Time
}

func normalizeCommunityShortsChannelSummaryRequest(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (communityShortsChannelSummaryRequest, error) {
	if cfg == nil {
		return communityShortsChannelSummaryRequest{}, fmt.Errorf("collect community shorts channel summary report: config is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	since = normalizeCommunityShortsSendCountTime(since)
	if since.IsZero() {
		return communityShortsChannelSummaryRequest{}, fmt.Errorf("collect community shorts channel summary report: since is empty")
	}
	if since.After(now) {
		return communityShortsChannelSummaryRequest{}, fmt.Errorf("collect community shorts channel summary report: since is after now")
	}

	return communityShortsChannelSummaryRequest{
		ctx:    ctx,
		logger: logger,
		now:    now,
		since:  since,
	}, nil
}

func BuildCommunityShortsChannelSummaryReport(
	rows []outbox.ChannelPostDeliverySummary,
	generatedAt time.Time,
	since time.Time,
) CommunityShortsChannelSummaryReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	since = normalizeCommunityShortsSendCountTime(since)

	normalizedRows := make([]outbox.ChannelPostDeliverySummary, 0, len(rows))
	summary := CommunityShortsChannelSummaryTotals{}
	for i := range rows {
		row := rows[i]
		row.ChannelID = strings.TrimSpace(row.ChannelID)
		row.EarliestObservedAt = normalizeCommunityShortsChannelSummaryTimePtr(row.EarliestObservedAt)
		row.LatestObservedAt = normalizeCommunityShortsChannelSummaryTimePtr(row.LatestObservedAt)
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
		left := communityShortsChannelSummarySortTime(normalizedRows[i])
		right := communityShortsChannelSummarySortTime(normalizedRows[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return normalizedRows[i].ChannelID < normalizedRows[j].ChannelID
	})

	return CommunityShortsChannelSummaryReport{
		GeneratedAt: generatedAt,
		WindowStart: since,
		WindowEnd:   generatedAt,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderCommunityShortsChannelSummaryMarkdown(report CommunityShortsChannelSummaryReport) string {
	var builder strings.Builder

	builder.WriteString(buildCommunityShortsChannelSummaryMetadataMarkdown(report))

	if len(report.Rows) == 0 {
		builder.WriteString("\n최근 윈도우에 해당하는 community/shorts 감지 채널이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString(buildCommunityShortsChannelSummaryTableMarkdown(report.Rows))
	return builder.String()
}

func buildCommunityShortsChannelSummaryMetadataMarkdown(report CommunityShortsChannelSummaryReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Channel Delivery Summary\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- window: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.WindowStart))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.WindowEnd))
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

func buildCommunityShortsChannelSummaryTableMarkdown(rows []outbox.ChannelPostDeliverySummary) string {
	var builder strings.Builder

	builder.WriteString("\n| status | channel_id | earliest_observed_at | latest_observed_at | detected_post_count | alarm_sent_post_count | success_post_count | failed_post_count | detected_unsent_post_count | community_detected_post_count | shorts_detected_post_count |\n")
	builder.WriteString("| --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for i := range rows {
		row := rows[i]
		builder.WriteString("| `")
		builder.WriteString(resolveCommunityShortsChannelSummaryStatus(row))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(row.ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.EarliestObservedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.LatestObservedAt))
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

func resolveCommunityShortsChannelSummaryStatus(row outbox.ChannelPostDeliverySummary) string {
	switch {
	case row.DetectedUnsentPostCount > 0 && row.FailedPostCount > 0:
		return "unsent_with_failures"
	case row.DetectedUnsentPostCount > 0:
		return "unsent_pending"
	case row.FailedPostCount > 0:
		return "failures_observed"
	default:
		return "ok"
	}
}

func normalizeCommunityShortsChannelSummaryTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := normalizeCommunityShortsSendCountTime(*value)
	return &normalized
}

func communityShortsChannelSummarySortTime(row outbox.ChannelPostDeliverySummary) time.Time {
	if row.LatestObservedAt != nil {
		return row.LatestObservedAt.UTC()
	}
	if row.EarliestObservedAt != nil {
		return row.EarliestObservedAt.UTC()
	}
	return time.Time{}
}

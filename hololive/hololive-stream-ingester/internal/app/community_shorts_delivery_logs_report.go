package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type CommunityShortsDeliveryLogQueryMode string

const (
	communityShortsDeliveryLogQueryModeRecent      CommunityShortsDeliveryLogQueryMode = "recent_window"
	communityShortsDeliveryLogQueryModeObservation CommunityShortsDeliveryLogQueryMode = "observation_window"
	communityShortsDeliveryLogDefaultLimit                                             = 200
)

type CommunityShortsDeliveryLogCollectOptions struct {
	Since                       *time.Time
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
	Limit                       int
}

type CommunityShortsDeliveryLogReport struct {
	GeneratedAt time.Time                         `json:"generated_at"`
	Query       CommunityShortsDeliveryLogQuery   `json:"query"`
	Summary     CommunityShortsDeliveryLogSummary `json:"summary"`
	Rows        []CommunityShortsDeliveryLogRow   `json:"rows"`
}

type CommunityShortsDeliveryLogQuery struct {
	Mode                        CommunityShortsDeliveryLogQueryMode `json:"mode"`
	WindowStart                 *time.Time                          `json:"window_start,omitempty"`
	WindowEnd                   *time.Time                          `json:"window_end,omitempty"`
	ObservationRuntimeName      string                              `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time                          `json:"observation_bigbang_cutover_at,omitempty"`
	Limit                       int                                 `json:"limit"`
	Truncated                   bool                                `json:"truncated"`
}

type CommunityShortsDeliveryLogSummary struct {
	LogCount        int `json:"log_count"`
	SuccessLogCount int `json:"success_log_count"`
	FailureLogCount int `json:"failure_log_count"`
	UniquePostCount int `json:"unique_post_count"`
	UniqueRoomCount int `json:"unique_room_count"`
}

type CommunityShortsDeliveryLogRow struct {
	domain.YouTubeNotificationDeliveryTelemetry
	PublishToEventMillis *int64 `json:"publish_to_event_millis,omitempty"`
	DetectToEventMillis  *int64 `json:"detect_to_event_millis,omitempty"`
}

func CollectCommunityShortsDeliveryLogReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsDeliveryLogCollectOptions,
) (CommunityShortsDeliveryLogReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCommunityShortsDeliveryLogCollectOptions(options, now)
	if err != nil {
		return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: %w", err)
	}

	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	db := databaseResources.Service.GetGormDB()
	telemetryRepo := outbox.NewDeliveryTelemetryRepository(db)

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	if query.Mode == communityShortsDeliveryLogQueryModeObservation {
		observationRepository := trackingrepo.NewRepository(db)
		state, stateErr := resolveCommunityShortsObservationQueryState(
			ctx,
			observationRepository,
			query.ObservationRuntimeName,
			*query.ObservationBigBangCutoverAt,
			now,
		)
		if stateErr != nil {
			return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: find observation window: %w", stateErr)
		}
		if state.Window == nil {
			return CommunityShortsDeliveryLogReport{}, fmt.Errorf(
				"collect community shorts delivery log report: observation window not found: runtime=%s cutover=%s",
				query.ObservationRuntimeName,
				formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
			)
		}
		query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
		query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)

		if state.Finalized {
			rows, err = telemetryRepo.ListByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: list finalized observation-window logs: %w", err)
			}
		} else {
			rows, err = telemetryRepo.ListByObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: list active observation-window logs: %w", err)
			}
		}
	} else {
		fetchLimit := query.Limit + 1
		rows, err = telemetryRepo.ListCommunityShortsDeliveryLogsSince(ctx, *query.WindowStart, fetchLimit)
		if err != nil {
			return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: list recent logs: %w", err)
		}
	}

	trimmedRows, truncated := trimCommunityShortsDeliveryLogRows(rows, query.Limit)
	query.Truncated = truncated

	return BuildCommunityShortsDeliveryLogReport(query, trimmedRows, now), nil
}

func BuildCommunityShortsDeliveryLogReport(
	query CommunityShortsDeliveryLogQuery,
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	generatedAt time.Time,
) CommunityShortsDeliveryLogReport {
	generatedAt = normalizeCommunityShortsSendCountTime(generatedAt)
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	query = normalizeCommunityShortsDeliveryLogQuery(query)

	normalizedRows := make([]CommunityShortsDeliveryLogRow, 0, len(rows))
	postSet := make(map[string]struct{}, len(rows))
	roomSet := make(map[string]struct{}, len(rows))
	summary := CommunityShortsDeliveryLogSummary{}

	for i := range rows {
		row := normalizeCommunityShortsDeliveryLogRow(rows[i])
		row.PublishToEventMillis = durationMillisToEvent(row.ActualPublishedAt, row.EventAt)
		row.DetectToEventMillis = durationMillisToEvent(row.DetectedAt, row.EventAt)
		normalizedRows = append(normalizedRows, row)

		summary.LogCount++
		if strings.EqualFold(strings.TrimSpace(row.SendResult), "success") {
			summary.SuccessLogCount++
		} else {
			summary.FailureLogCount++
		}
		postSet[buildCommunityShortsDeliveryLogPostKey(row)] = struct{}{}
		if roomID := strings.TrimSpace(row.RoomID); roomID != "" {
			roomSet[roomID] = struct{}{}
		}
	}

	summary.UniquePostCount = len(postSet)
	summary.UniqueRoomCount = len(roomSet)

	sort.SliceStable(normalizedRows, func(i, j int) bool {
		leftSortTime := communityShortsDeliveryLogSortTime(normalizedRows[i])
		rightSortTime := communityShortsDeliveryLogSortTime(normalizedRows[j])
		if !leftSortTime.Equal(rightSortTime) {
			return leftSortTime.After(rightSortTime)
		}
		leftPostID := resolveCommunityShortsDeliveryLogPostID(normalizedRows[i])
		rightPostID := resolveCommunityShortsDeliveryLogPostID(normalizedRows[j])
		if leftPostID != rightPostID {
			return leftPostID < rightPostID
		}
		if !normalizedRows[i].EventAt.Equal(normalizedRows[j].EventAt) {
			return normalizedRows[i].EventAt.Before(normalizedRows[j].EventAt)
		}
		return normalizedRows[i].ID < normalizedRows[j].ID
	})

	return CommunityShortsDeliveryLogReport{
		GeneratedAt: generatedAt,
		Query:       query,
		Summary:     summary,
		Rows:        normalizedRows,
	}
}

func RenderCommunityShortsDeliveryLogMarkdown(report CommunityShortsDeliveryLogReport) string {
	var builder strings.Builder

	builder.WriteString("# YouTube Community/Shorts Delivery Logs Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- mode: `")
	builder.WriteString(string(report.Query.Mode))
	builder.WriteString("`\n")
	if report.Query.WindowStart != nil || report.Query.WindowEnd != nil {
		builder.WriteString("- window: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowStart))
		builder.WriteString("` -> `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.WindowEnd))
		builder.WriteString("`\n")
	}
	if report.Query.Mode == communityShortsDeliveryLogQueryModeObservation {
		builder.WriteString("- observation runtime: `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(report.Query.ObservationRuntimeName))
		builder.WriteString("`, cutover: `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(report.Query.ObservationBigBangCutoverAt))
		builder.WriteString("`\n")
	}
	builder.WriteString("- summary: logs=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.LogCount))
	builder.WriteString("`, success_logs=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.SuccessLogCount))
	builder.WriteString("`, failure_logs=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.FailureLogCount))
	builder.WriteString("`, unique_posts=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UniquePostCount))
	builder.WriteString("`, unique_rooms=`")
	builder.WriteString(fmt.Sprintf("%d", report.Summary.UniqueRoomCount))
	builder.WriteString("`, limit=`")
	builder.WriteString(fmt.Sprintf("%d", report.Query.Limit))
	builder.WriteString("`, truncated=`")
	builder.WriteString(fmt.Sprintf("%t", report.Query.Truncated))
	builder.WriteString("`\n")

	if len(report.Rows) == 0 {
		builder.WriteString("\n조회된 community/shorts 발송 로그가 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| alarm_type | channel_id | post_id | room_id | attempt | actual_published_at | detected_at | alarm_sent_at | alarm_latency_millis | event_at | publish_to_event_ms | send_result | delivery_path | observation_status | failure_reason |\n")
	builder.WriteString("| --- | --- | --- | --- | ---: | --- | --- | --- | ---: | --- | ---: | --- | --- | --- | --- |\n")
	for i := range report.Rows {
		row := report.Rows[i]
		builder.WriteString("| `")
		builder.WriteString(string(row.AlarmType))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.ChannelID)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(resolveCommunityShortsDeliveryLogPostID(row)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.RoomID)))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", row.AttemptOrdinal))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.ActualPublishedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.DetectedAt))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsSendCountTimePtr(row.AlarmSentAt))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.AlarmLatencyMillis))
		builder.WriteString(" | `")
		builder.WriteString(formatCommunityShortsSendCountTime(row.EventAt))
		builder.WriteString("` | ")
		builder.WriteString(formatCommunityShortsSendCountInt64Ptr(row.PublishToEventMillis))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.SendResult)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.DeliveryPath)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.ObservationStatus)))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(strings.TrimSpace(row.FailureReason)))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func normalizeCommunityShortsDeliveryLogCollectOptions(
	options CommunityShortsDeliveryLogCollectOptions,
	now time.Time,
) (CommunityShortsDeliveryLogQuery, error) {
	limit := normalizeCommunityShortsDeliveryLogRequestedLimit(options.Limit)
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)
	hasObservationCutover := options.ObservationBigBangCutoverAt != nil && !options.ObservationBigBangCutoverAt.IsZero()
	hasObservationQuery := observationRuntimeName != "" || hasObservationCutover
	hasRecentQuery := options.Since != nil && !options.Since.IsZero()

	if hasObservationQuery {
		if hasRecentQuery {
			return CommunityShortsDeliveryLogQuery{}, fmt.Errorf("recent window and observation window are mutually exclusive")
		}
		if observationRuntimeName == "" || !hasObservationCutover {
			return CommunityShortsDeliveryLogQuery{}, fmt.Errorf("observation runtime name and cutover must both be set")
		}
		return CommunityShortsDeliveryLogQuery{
			Mode:                        communityShortsDeliveryLogQueryModeObservation,
			ObservationRuntimeName:      observationRuntimeName,
			ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
			Limit:                       limit,
		}, nil
	}

	if !hasRecentQuery {
		return CommunityShortsDeliveryLogQuery{}, fmt.Errorf("recent window since is empty")
	}
	since := normalizeCommunityShortsSendCountTime(*options.Since)
	if since.IsZero() {
		return CommunityShortsDeliveryLogQuery{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return CommunityShortsDeliveryLogQuery{}, fmt.Errorf("recent window since is after now")
	}

	return CommunityShortsDeliveryLogQuery{
		Mode:        communityShortsDeliveryLogQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&now),
		Limit:       limit,
	}, nil
}

func normalizeCommunityShortsDeliveryLogRequestedLimit(limit int) int {
	if limit <= 0 {
		return communityShortsDeliveryLogDefaultLimit
	}
	if limit > outboxCommunityShortsDeliveryLogMaxLimit() {
		return outboxCommunityShortsDeliveryLogMaxLimit()
	}
	return limit
}

func outboxCommunityShortsDeliveryLogMaxLimit() int {
	return 5000
}

func trimCommunityShortsDeliveryLogRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	limit int,
) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	trimmed := append([]domain.YouTubeNotificationDeliveryTelemetry(nil), rows[:limit]...)
	return trimmed, true
}

func normalizeCommunityShortsDeliveryLogQuery(query CommunityShortsDeliveryLogQuery) CommunityShortsDeliveryLogQuery {
	query.Mode = CommunityShortsDeliveryLogQueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = cloneCommunityShortsSendCountTime(query.WindowStart)
	query.WindowEnd = cloneCommunityShortsSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func normalizeCommunityShortsDeliveryLogRow(row domain.YouTubeNotificationDeliveryTelemetry) CommunityShortsDeliveryLogRow {
	row.ChannelID = strings.TrimSpace(row.ChannelID)
	row.ContentID = strings.TrimSpace(row.ContentID)
	row.PostID = strings.TrimSpace(row.PostID)
	row.RoomID = strings.TrimSpace(row.RoomID)
	row.DedupeKey = strings.TrimSpace(row.DedupeKey)
	row.DeliveryPath = strings.TrimSpace(row.DeliveryPath)
	row.DeliveryMode = strings.TrimSpace(row.DeliveryMode)
	row.SendResult = strings.TrimSpace(row.SendResult)
	row.FailureReason = strings.TrimSpace(row.FailureReason)
	row.ObservationStatus = strings.TrimSpace(row.ObservationStatus)
	row.ObservationRuntimeName = strings.TrimSpace(row.ObservationRuntimeName)
	row.ActualPublishedAt = cloneCommunityShortsSendCountTime(row.ActualPublishedAt)
	row.AlarmSentAt = cloneCommunityShortsSendCountTime(row.AlarmSentAt)
	row.AlarmLatencyMillis = cloneCommunityShortsSendCountInt64(row.AlarmLatencyMillis)
	row.DetectedAt = cloneCommunityShortsSendCountTime(row.DetectedAt)
	row.ObservationBigBangCutoverAt = cloneCommunityShortsSendCountTime(row.ObservationBigBangCutoverAt)
	row.ObservationStartedAt = cloneCommunityShortsSendCountTime(row.ObservationStartedAt)
	row.ObservationEndedAt = cloneCommunityShortsSendCountTime(row.ObservationEndedAt)
	row.AttemptStartedAt = cloneCommunityShortsSendCountTime(row.AttemptStartedAt)
	row.AttemptFinishedAt = cloneCommunityShortsSendCountTime(row.AttemptFinishedAt)
	row.EventAt = normalizeCommunityShortsSendCountTime(row.EventAt)
	row.NextAttemptAt = normalizeCommunityShortsSendCountTime(row.NextAttemptAt)
	row.CreatedAt = normalizeCommunityShortsSendCountTime(row.CreatedAt)
	row.LockedAt = cloneCommunityShortsSendCountTime(row.LockedAt)
	row.LoggedAt = cloneCommunityShortsSendCountTime(row.LoggedAt)
	return CommunityShortsDeliveryLogRow{YouTubeNotificationDeliveryTelemetry: row}
}

func buildCommunityShortsDeliveryLogPostKey(row CommunityShortsDeliveryLogRow) string {
	return strings.Join([]string{
		string(row.AlarmType),
		strings.TrimSpace(row.ChannelID),
		resolveCommunityShortsDeliveryLogPostID(row),
	}, "::")
}

func resolveCommunityShortsDeliveryLogPostID(row CommunityShortsDeliveryLogRow) string {
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func communityShortsDeliveryLogSortTime(row CommunityShortsDeliveryLogRow) time.Time {
	for _, candidate := range []*time.Time{row.ActualPublishedAt, row.DetectedAt} {
		if candidate != nil {
			return candidate.UTC()
		}
	}
	return row.EventAt.UTC()
}

func durationMillisToEvent(start *time.Time, eventAt time.Time) *int64 {
	if start == nil || eventAt.IsZero() {
		return nil
	}
	millis := eventAt.UTC().Sub(start.UTC()).Milliseconds()
	return &millis
}

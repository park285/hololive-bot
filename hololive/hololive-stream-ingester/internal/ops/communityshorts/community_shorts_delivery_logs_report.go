package communityshortsops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
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

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectCommunityShortsDeliveryLogReportWithSession(ctx, session, query, now)
}

func collectCommunityShortsDeliveryLogReportWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsDeliveryLogQuery,
	now time.Time,
) (CommunityShortsDeliveryLogReport, error) {
	if session == nil {
		return CommunityShortsDeliveryLogReport{}, fmt.Errorf("collect community shorts delivery log report: session is nil")
	}

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	var err error
	if query.Mode == communityShortsDeliveryLogQueryModeObservation {
		query, rows, err = listCommunityShortsDeliveryObservationLogs(ctx, session, query, now)
	} else {
		rows, err = listCommunityShortsDeliveryRecentLogs(ctx, session, query)
	}
	if err != nil {
		return CommunityShortsDeliveryLogReport{}, err
	}

	trimmedRows, truncated := trimCommunityShortsDeliveryLogRows(rows, query.Limit)
	query.Truncated = truncated

	return BuildCommunityShortsDeliveryLogReport(query, trimmedRows, now), nil
}

func listCommunityShortsDeliveryObservationLogs(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsDeliveryLogQuery,
	now time.Time,
) (CommunityShortsDeliveryLogQuery, []domain.YouTubeNotificationDeliveryTelemetry, error) {
	state, err := resolveCommunityShortsDeliveryLogObservationState(ctx, session, query, now)
	if err != nil {
		return query, nil, err
	}
	query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)

	rows, err := listCommunityShortsDeliveryLogsForObservationState(ctx, session, query, state)
	return query, rows, err
}

func resolveCommunityShortsDeliveryLogObservationState(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsDeliveryLogQuery,
	now time.Time,
) (communityShortsObservationQueryState, error) {
	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		session.trackingRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return communityShortsObservationQueryState{}, fmt.Errorf("collect community shorts delivery log report: find observation window: %w", err)
	}
	if state.Window == nil {
		return communityShortsObservationQueryState{}, fmt.Errorf(
			"collect community shorts delivery log report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return state, nil
}

func listCommunityShortsDeliveryLogsForObservationState(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsDeliveryLogQuery,
	state communityShortsObservationQueryState,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if state.Finalized {
		rows, err := session.telemetryRepo.ListByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
		if err != nil {
			return nil, fmt.Errorf("collect community shorts delivery log report: list finalized observation-window logs: %w", err)
		}
		return rows, nil
	}

	rows, err := session.telemetryRepo.ListByObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts delivery log report: list active observation-window logs: %w", err)
	}
	return rows, nil
}

func listCommunityShortsDeliveryRecentLogs(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsDeliveryLogQuery,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	fetchLimit := query.Limit + 1
	rows, err := session.telemetryRepo.ListCommunityShortsDeliveryLogsSince(ctx, *query.WindowStart, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts delivery log report: list recent logs: %w", err)
	}
	return rows, nil
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
		return normalizeCommunityShortsDeliveryLogObservationOptions(options, observationRuntimeName, hasObservationCutover, hasRecentQuery, limit)
	}

	return normalizeCommunityShortsDeliveryLogRecentOptions(options, hasRecentQuery, limit, now)
}

func normalizeCommunityShortsDeliveryLogObservationOptions(
	options CommunityShortsDeliveryLogCollectOptions,
	observationRuntimeName string,
	hasObservationCutover bool,
	hasRecentQuery bool,
	limit int,
) (CommunityShortsDeliveryLogQuery, error) {
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

func normalizeCommunityShortsDeliveryLogRecentOptions(
	options CommunityShortsDeliveryLogCollectOptions,
	hasRecentQuery bool,
	limit int,
	now time.Time,
) (CommunityShortsDeliveryLogQuery, error) {
	since, err := normalizeCommunityShortsDeliveryLogRecentSince(options.Since, hasRecentQuery, now)
	if err != nil {
		return CommunityShortsDeliveryLogQuery{}, err
	}

	return CommunityShortsDeliveryLogQuery{
		Mode:        communityShortsDeliveryLogQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&now),
		Limit:       limit,
	}, nil
}

func normalizeCommunityShortsDeliveryLogRecentSince(sinceValue *time.Time, hasRecentQuery bool, now time.Time) (time.Time, error) {
	if !hasRecentQuery {
		return time.Time{}, fmt.Errorf("recent window since is empty")
	}
	since := normalizeCommunityShortsSendCountTime(*sinceValue)
	if since.IsZero() {
		return time.Time{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return time.Time{}, fmt.Errorf("recent window since is after now")
	}
	return since, nil
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

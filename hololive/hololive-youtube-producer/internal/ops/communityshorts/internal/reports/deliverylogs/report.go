package deliverylogs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type QueryMode string

const (
	QueryModeRecent      QueryMode = "recent_window"
	QueryModeObservation QueryMode = "observation_window"
	DefaultLimit                   = 200
)

type CollectOptions struct {
	Since                       *time.Time
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
	Limit                       int
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Query       Query     `json:"query"`
	Summary     Summary   `json:"summary"`
	Rows        []Row     `json:"rows"`
}

type Query struct {
	Mode                        QueryMode  `json:"mode"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
	ObservationRuntimeName      string     `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	Limit                       int        `json:"limit"`
	Truncated                   bool       `json:"truncated"`
}

type Summary struct {
	LogCount        int `json:"log_count"`
	SuccessLogCount int `json:"success_log_count"`
	FailureLogCount int `json:"failure_log_count"`
	UniquePostCount int `json:"unique_post_count"`
	UniqueRoomCount int `json:"unique_room_count"`
}

type Row struct {
	domain.YouTubeNotificationDeliveryTelemetry
	PublishToEventMillis *int64 `json:"publish_to_event_millis,omitempty"`
	DetectToEventMillis  *int64 `json:"detect_to_event_millis,omitempty"`
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if appConfig == nil {
		return Report{}, fmt.Errorf("collect community shorts delivery log report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCollectOptions(options, now)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts delivery log report: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts delivery log report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectWithSession(ctx, session, query, now)
}

func collectWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
) (Report, error) {
	if session == nil {
		return Report{}, fmt.Errorf("collect community shorts delivery log report: session is nil")
	}

	var rows []domain.YouTubeNotificationDeliveryTelemetry
	var err error
	if query.Mode == QueryModeObservation {
		query, rows, err = listObservationLogs(ctx, session, query, now)
	} else {
		rows, err = listRecentLogs(ctx, session, query)
	}
	if err != nil {
		return Report{}, err
	}

	trimmedRows, truncated := trimRows(rows, query.Limit)
	query.Truncated = truncated

	return Build(query, trimmedRows, now), nil
}

func listObservationLogs(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
) (Query, []domain.YouTubeNotificationDeliveryTelemetry, error) {
	state, err := resolveObservationState(ctx, session, query, now)
	if err != nil {
		return query, nil, err
	}
	query.WindowStart = shared.CloneSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&state.EffectiveWindowEnd)

	rows, err := listLogsForObservationState(ctx, session, query, state)
	return query, rows, err
}

func resolveObservationState(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
) (shared.ObservationQueryState, error) {
	state, err := shared.ResolveObservationQueryState(
		ctx,
		session.TrackingRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return shared.ObservationQueryState{}, fmt.Errorf("collect community shorts delivery log report: find observation window: %w", err)
	}
	if state.Window == nil {
		return shared.ObservationQueryState{}, fmt.Errorf(
			"collect community shorts delivery log report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return state, nil
}

func listLogsForObservationState(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	state shared.ObservationQueryState,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	if state.Finalized {
		rows, err := session.TelemetryRepository.ListByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
		if err != nil {
			return nil, fmt.Errorf("collect community shorts delivery log report: list finalized observation-window logs: %w", err)
		}
		return rows, nil
	}

	rows, err := session.TelemetryRepository.ListByObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts delivery log report: list active observation-window logs: %w", err)
	}
	return rows, nil
}

func listRecentLogs(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
) ([]domain.YouTubeNotificationDeliveryTelemetry, error) {
	fetchLimit := query.Limit + 1
	rows, err := session.TelemetryRepository.ListCommunityShortsDeliveryLogsSince(ctx, *query.WindowStart, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts delivery log report: list recent logs: %w", err)
	}
	return rows, nil
}

func normalizeCollectOptions(
	options CollectOptions,
	now time.Time,
) (Query, error) {
	limit := normalizeRequestedLimit(options.Limit)
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)
	hasObservationCutover := options.ObservationBigBangCutoverAt != nil && !options.ObservationBigBangCutoverAt.IsZero()
	hasObservationQuery := observationRuntimeName != "" || hasObservationCutover
	hasRecentQuery := options.Since != nil && !options.Since.IsZero()

	if hasObservationQuery {
		return normalizeObservationOptions(options, observationRuntimeName, hasObservationCutover, hasRecentQuery, limit)
	}

	return normalizeRecentOptions(options, hasRecentQuery, limit, now)
}

func normalizeObservationOptions(
	options CollectOptions,
	observationRuntimeName string,
	hasObservationCutover bool,
	hasRecentQuery bool,
	limit int,
) (Query, error) {
	if hasRecentQuery {
		return Query{}, fmt.Errorf("recent window and observation window are mutually exclusive")
	}
	if observationRuntimeName == "" || !hasObservationCutover {
		return Query{}, fmt.Errorf("observation runtime name and cutover must both be set")
	}
	return Query{
		Mode:                        QueryModeObservation,
		ObservationRuntimeName:      observationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(options.ObservationBigBangCutoverAt),
		Limit:                       limit,
	}, nil
}

func normalizeRecentOptions(
	options CollectOptions,
	hasRecentQuery bool,
	limit int,
	now time.Time,
) (Query, error) {
	since, err := normalizeRecentSince(options.Since, hasRecentQuery, now)
	if err != nil {
		return Query{}, err
	}

	return Query{
		Mode:        QueryModeRecent,
		WindowStart: shared.CloneSendCountTime(&since),
		WindowEnd:   shared.CloneSendCountTime(&now),
		Limit:       limit,
	}, nil
}

func normalizeRecentSince(sinceValue *time.Time, hasRecentQuery bool, now time.Time) (time.Time, error) {
	if !hasRecentQuery {
		return time.Time{}, fmt.Errorf("recent window since is empty")
	}
	since := shared.NormalizeSendCountTime(*sinceValue)
	if since.IsZero() {
		return time.Time{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return time.Time{}, fmt.Errorf("recent window since is after now")
	}
	return since, nil
}

func normalizeRequestedLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > maxLimit() {
		return maxLimit()
	}
	return limit
}

func maxLimit() int {
	return 5000
}

func trimRows(
	rows []domain.YouTubeNotificationDeliveryTelemetry,
	limit int,
) ([]domain.YouTubeNotificationDeliveryTelemetry, bool) {
	if limit <= 0 || len(rows) <= limit {
		return rows, false
	}
	trimmed := append([]domain.YouTubeNotificationDeliveryTelemetry(nil), rows[:limit]...)
	return trimmed, true
}

func normalizeQuery(query Query) Query {
	query.Mode = QueryMode(strings.TrimSpace(string(query.Mode)))
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	return query
}

func normalizeRow(row domain.YouTubeNotificationDeliveryTelemetry) Row {
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
	row.ActualPublishedAt = shared.CloneSendCountTime(row.ActualPublishedAt)
	row.AlarmSentAt = shared.CloneSendCountTime(row.AlarmSentAt)
	row.AlarmLatencyMillis = shared.CloneSendCountInt64(row.AlarmLatencyMillis)
	row.DetectedAt = shared.CloneSendCountTime(row.DetectedAt)
	row.ObservationBigBangCutoverAt = shared.CloneSendCountTime(row.ObservationBigBangCutoverAt)
	row.ObservationStartedAt = shared.CloneSendCountTime(row.ObservationStartedAt)
	row.ObservationEndedAt = shared.CloneSendCountTime(row.ObservationEndedAt)
	row.AttemptStartedAt = shared.CloneSendCountTime(row.AttemptStartedAt)
	row.AttemptFinishedAt = shared.CloneSendCountTime(row.AttemptFinishedAt)
	row.EventAt = shared.NormalizeSendCountTime(row.EventAt)
	row.NextAttemptAt = shared.NormalizeSendCountTime(row.NextAttemptAt)
	row.CreatedAt = shared.NormalizeSendCountTime(row.CreatedAt)
	row.LockedAt = shared.CloneSendCountTime(row.LockedAt)
	row.LoggedAt = shared.CloneSendCountTime(row.LoggedAt)
	return Row{YouTubeNotificationDeliveryTelemetry: row}
}

func buildPostKey(row Row) string {
	return strings.Join([]string{
		string(row.AlarmType),
		strings.TrimSpace(row.ChannelID),
		resolvePostID(row),
	}, "::")
}

func resolvePostID(row Row) string {
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func rowSortTime(row Row) time.Time {
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

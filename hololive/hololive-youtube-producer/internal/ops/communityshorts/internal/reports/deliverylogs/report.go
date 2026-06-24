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

const QueryModeRecent QueryMode = "recent_window"

const DefaultLimit = 200

type CollectOptions struct {
	Since *time.Time
	Limit int
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Query       Query     `json:"query"`
	Summary     Summary   `json:"summary"`
	Rows        []Row     `json:"rows"`
}

type Query struct {
	Mode        QueryMode  `json:"mode"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
	Limit       int        `json:"limit"`
	Truncated   bool       `json:"truncated"`
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
		return Report{}, fmt.Errorf("collect community shorts delivery log report: context is nil")
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

	rows, err := listRecentLogs(ctx, session, query)
	if err != nil {
		return Report{}, err
	}

	trimmedRows, truncated := trimRows(rows, query.Limit)
	query.Truncated = truncated

	return Build(query, trimmedRows, now), nil
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
	hasRecentQuery := options.Since != nil && !options.Since.IsZero()

	return normalizeRecentOptions(options, hasRecentQuery, limit, now)
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
	return query
}

func normalizeRow(row *domain.YouTubeNotificationDeliveryTelemetry) Row {
	if row == nil {
		return Row{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.RoomID = strings.TrimSpace(normalized.RoomID)
	normalized.DedupeKey = strings.TrimSpace(normalized.DedupeKey)
	normalized.DeliveryPath = strings.TrimSpace(normalized.DeliveryPath)
	normalized.DeliveryMode = strings.TrimSpace(normalized.DeliveryMode)
	normalized.SendResult = strings.TrimSpace(normalized.SendResult)
	normalized.FailureReason = strings.TrimSpace(normalized.FailureReason)
	normalized.ObservationStatus = strings.TrimSpace(normalized.ObservationStatus)
	normalized.ObservationRuntimeName = strings.TrimSpace(normalized.ObservationRuntimeName)
	normalized.ActualPublishedAt = shared.CloneSendCountTime(normalized.ActualPublishedAt)
	normalized.AlarmSentAt = shared.CloneSendCountTime(normalized.AlarmSentAt)
	normalized.AlarmLatencyMillis = shared.CloneSendCountInt64(normalized.AlarmLatencyMillis)
	normalized.DetectedAt = shared.CloneSendCountTime(normalized.DetectedAt)
	normalized.ObservationBigBangCutoverAt = shared.CloneSendCountTime(normalized.ObservationBigBangCutoverAt)
	normalized.ObservationStartedAt = shared.CloneSendCountTime(normalized.ObservationStartedAt)
	normalized.ObservationEndedAt = shared.CloneSendCountTime(normalized.ObservationEndedAt)
	normalized.AttemptStartedAt = shared.CloneSendCountTime(normalized.AttemptStartedAt)
	normalized.AttemptFinishedAt = shared.CloneSendCountTime(normalized.AttemptFinishedAt)
	normalized.EventAt = shared.NormalizeSendCountTime(normalized.EventAt)
	normalized.NextAttemptAt = shared.NormalizeSendCountTime(normalized.NextAttemptAt)
	normalized.CreatedAt = shared.NormalizeSendCountTime(normalized.CreatedAt)
	normalized.LockedAt = shared.CloneSendCountTime(normalized.LockedAt)
	normalized.LoggedAt = shared.CloneSendCountTime(normalized.LoggedAt)
	return Row{YouTubeNotificationDeliveryTelemetry: normalized}
}

func buildPostKey(row *Row) string {
	if row == nil {
		return ""
	}
	return strings.Join([]string{
		string(row.AlarmType),
		strings.TrimSpace(row.ChannelID),
		resolvePostID(row),
	}, "::")
}

func resolvePostID(row *Row) string {
	if row == nil {
		return ""
	}
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func rowSortTime(row *Row) time.Time {
	if row == nil {
		return time.Time{}
	}
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

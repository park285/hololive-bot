package sendstate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type PerPostState string

const (
	PerPostStateSent                    PerPostState = "sent"
	PerPostStateAttemptedWithoutSuccess PerPostState = "attempted_without_success"
	PerPostStateNotSent                 PerPostState = "not_sent"
)

type CollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type Query struct {
	ObservationRuntimeName      string     `json:"observation_runtime_name"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
	Finalized                   bool       `json:"finalized"`
}

type Summary struct {
	PostStateCount                   int        `json:"post_state_count"`
	SentPostCount                    int        `json:"sent_post_count"`
	AttemptedWithoutSuccessPostCount int        `json:"attempted_without_success_post_count"`
	NotSentPostCount                 int        `json:"not_sent_post_count"`
	DuplicateSuccessPostCount        int        `json:"duplicate_success_post_count"`
	FailedAttemptPostCount           int        `json:"failed_attempt_post_count"`
	CommunityPostCount               int        `json:"community_post_count"`
	ShortsPostCount                  int        `json:"shorts_post_count"`
	EarliestObservedAt               *time.Time `json:"earliest_observed_at,omitempty"`
	LatestObservedAt                 *time.Time `json:"latest_observed_at,omitempty"`
	EarliestAlarmSentAt              *time.Time `json:"earliest_alarm_sent_at,omitempty"`
	LatestAlarmSentAt                *time.Time `json:"latest_alarm_sent_at,omitempty"`
}

type Row struct {
	outbox.PostSendCount
	SendState               PerPostState     `json:"send_state"`
	PostKey                 string           `json:"post_key,omitempty"`
	ReportAlarmType         domain.AlarmType `json:"alarm_type"`
	ReportChannelID         string           `json:"channel_id"`
	ReportPostID            string           `json:"post_id"`
	ReportActualPublishedAt *time.Time       `json:"actual_published_at,omitempty"`
	ReportDetectedAt        *time.Time       `json:"detected_at,omitempty"`
	ReportAlarmSentAt       *time.Time       `json:"alarm_sent_at,omitempty"`
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Query       Query     `json:"query"`
	Summary     Summary   `json:"summary"`
	Rows        []Row     `json:"rows"`
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
) (Report, error) {
	ctx, logger, now, query, err := prepareCollectInputs(ctx, appConfig, logger, now, options)
	if err != nil {
		return Report{}, err
	}

	session, cleanupDB, err := openSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, err
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	state, err := resolveObservationState(ctx, session, query, now)
	if err != nil {
		return Report{}, err
	}

	query.WindowStart = shared.CloneSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&state.EffectiveWindowEnd)
	query.Finalized = state.Finalized

	rows, err := listRows(ctx, session, query, state)
	if err != nil {
		return Report{}, err
	}

	return Build(rows, query, now), nil
}

func prepareCollectInputs(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
) (context.Context, *slog.Logger, time.Time, Query, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if appConfig == nil {
		return nil, nil, time.Time{}, Query{}, fmt.Errorf("collect community shorts send state report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCollectOptions(options)
	if err != nil {
		return nil, nil, time.Time{}, Query{}, fmt.Errorf("collect community shorts send state report: %w", err)
	}

	return ctx, logger, now, query, nil
}

func openSession(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
) (*shared.OpsSession, func(), error) {
	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("collect community shorts send state report: %w", err)
	}
	if session == nil {
		if cleanupDB != nil {
			cleanupDB()
		}
		return nil, nil, fmt.Errorf("collect community shorts send state report: session is nil")
	}
	return session, cleanupDB, nil
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
		return shared.ObservationQueryState{}, fmt.Errorf("collect community shorts send state report: find observation window: %w", err)
	}
	if state.Window == nil {
		return shared.ObservationQueryState{}, fmt.Errorf(
			"collect community shorts send state report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}
	return state, nil
}

func listRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	state shared.ObservationQueryState,
) ([]outbox.PostSendCount, error) {
	if state.Finalized {
		rows, err := session.TelemetryRepository.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
		if err != nil {
			return nil, fmt.Errorf("collect community shorts send state report: list finalized observation-window send states: %w", err)
		}
		return rows, nil
	}

	rows, err := session.TelemetryRepository.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return nil, fmt.Errorf("collect community shorts send state report: list active observation-window send states: %w", err)
	}
	return rows, nil
}

func normalizeCollectOptions(
	options CollectOptions,
) (Query, error) {
	runtimeName := strings.TrimSpace(options.ObservationRuntimeName)
	cutoverAt := shared.CloneSendCountTime(options.ObservationBigBangCutoverAt)
	if runtimeName == "" || cutoverAt == nil || cutoverAt.IsZero() {
		return Query{}, fmt.Errorf("observation runtime name and big-bang cutover at are required")
	}

	return Query{
		ObservationRuntimeName:      runtimeName,
		ObservationBigBangCutoverAt: cutoverAt,
	}, nil
}

func normalizeQuery(query Query) Query {
	query.ObservationRuntimeName = strings.TrimSpace(query.ObservationRuntimeName)
	query.ObservationBigBangCutoverAt = shared.CloneSendCountTime(query.ObservationBigBangCutoverAt)
	query.WindowStart = shared.CloneSendCountTime(query.WindowStart)
	query.WindowEnd = shared.CloneSendCountTime(query.WindowEnd)
	return query
}

func resolvePerPostState(row *outbox.PostSendCount) PerPostState {
	if hasSuccess(row) {
		return PerPostStateSent
	}
	if hasAttempt(row) {
		return PerPostStateAttemptedWithoutSuccess
	}
	return PerPostStateNotSent
}

func hasSuccess(row *outbox.PostSendCount) bool {
	if row == nil {
		return false
	}
	return row.SuccessSendCount > 0 || row.AlarmSentAt != nil || row.FirstSuccessAt != nil || row.LastSuccessAt != nil
}

func hasAttempt(row *outbox.PostSendCount) bool {
	if row == nil {
		return false
	}
	return row.OutboxCount > 0 || row.FailedAttemptCount > 0 || row.FirstEventAt != nil || row.LastEventAt != nil
}

func resolvePostID(row *outbox.PostSendCount) string {
	if row == nil {
		return ""
	}
	if strings.TrimSpace(row.PostID) != "" {
		return strings.TrimSpace(row.PostID)
	}
	return strings.TrimSpace(row.ContentID)
}

func resolveAlarmSentAt(row *outbox.PostSendCount) *time.Time {
	if row == nil {
		return nil
	}
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.LastSuccessAt} {
		if candidate != nil {
			return shared.CloneSendCountTime(candidate)
		}
	}
	return nil
}

func resolveObservedAt(row *Row) *time.Time {
	if row == nil {
		return nil
	}
	for _, candidate := range []*time.Time{row.ReportActualPublishedAt, row.ReportDetectedAt, row.LastEventAt, row.ReportAlarmSentAt} {
		if candidate != nil {
			return shared.CloneSendCountTime(candidate)
		}
	}
	return nil
}

func sortTime(row *Row) time.Time {
	if observedAt := resolveObservedAt(row); observedAt != nil {
		return observedAt.UTC()
	}
	return time.Time{}
}

func buildPostKey(alarmType domain.AlarmType, channelID, postID string) string {
	trimmedChannelID := strings.TrimSpace(channelID)
	trimmedPostID := strings.TrimSpace(postID)
	if !alarmType.IsValid() || trimmedChannelID == "" || trimmedPostID == "" {
		return ""
	}
	return strings.Join([]string{string(alarmType), trimmedChannelID, trimmedPostID}, "|")
}

func normalizePostSendCount(row *outbox.PostSendCount) outbox.PostSendCount {
	if row == nil {
		return outbox.PostSendCount{}
	}
	normalized := *row
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.PostID = strings.TrimSpace(normalized.PostID)
	normalized.ContentID = strings.TrimSpace(normalized.ContentID)
	normalized.ActualPublishedAt = shared.CloneSendCountTime(normalized.ActualPublishedAt)
	normalized.DetectedAt = shared.CloneSendCountTime(normalized.DetectedAt)
	normalized.AlarmSentAt = shared.CloneSendCountTime(normalized.AlarmSentAt)
	normalized.FirstEventAt = shared.CloneSendCountTime(normalized.FirstEventAt)
	normalized.LastEventAt = shared.CloneSendCountTime(normalized.LastEventAt)
	normalized.FirstSuccessAt = shared.CloneSendCountTime(normalized.FirstSuccessAt)
	normalized.LastSuccessAt = shared.CloneSendCountTime(normalized.LastSuccessAt)
	return normalized
}

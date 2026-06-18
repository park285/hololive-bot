package sendcounts

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

type QueryMode string

const (
	QueryModeRecent        QueryMode          = "recent_window"
	QueryModeObservation   QueryMode          = "observation_window"
	VerificationStatusPass VerificationStatus = "pass"
	VerificationStatusFail VerificationStatus = "fail"
	DuplicateAlarmRule                        = "duplicate_success_posts == 0"
)

type CollectOptions struct {
	Since                       *time.Time
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type Query struct {
	Mode                        QueryMode  `json:"mode"`
	WindowStart                 *time.Time `json:"window_start,omitempty"`
	WindowEnd                   *time.Time `json:"window_end,omitempty"`
	ObservationRuntimeName      string     `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time `json:"observation_bigbang_cutover_at,omitempty"`
}

type Report struct {
	GeneratedAt  time.Time    `json:"generated_at"`
	Query        Query        `json:"query"`
	WindowStart  time.Time    `json:"window_start"`
	WindowEnd    time.Time    `json:"window_end"`
	Summary      Summary      `json:"summary"`
	Verification Verification `json:"verification"`
	Rows         []Row        `json:"rows"`
}

type Summary struct {
	PostCount                         int `json:"post_count"`
	SuccessfulPostCount               int `json:"successful_post_count"`
	ZeroSuccessPostCount              int `json:"zero_success_post_count"`
	DuplicateSuccessPostCount         int `json:"duplicate_success_post_count"`
	FailedAttemptPostCount            int `json:"failed_attempt_post_count"`
	OutboxMissingPostCount            int `json:"outbox_missing_post_count"`
	ExternalCollectionSourcePostCount int `json:"external_collection_source_post_count"`
	InternalDeliverySourcePostCount   int `json:"internal_delivery_source_post_count"`
	MixedDelaySourcePostCount         int `json:"mixed_delay_source_post_count"`
	QueueWaitCausePostCount           int `json:"queue_wait_cause_post_count"`
	RetryAccumulationCausePostCount   int `json:"retry_accumulation_cause_post_count"`
	JobFailureCausePostCount          int `json:"job_failure_cause_post_count"`
}

type VerificationStatus string

type Verification struct {
	DuplicateAlarmStatus    VerificationStatus `json:"duplicate_alarm_status"`
	DuplicateAlarmPostCount int                `json:"duplicate_alarm_post_count"`
	DuplicateAlarmRule      string             `json:"duplicate_alarm_rule"`
}

type Row struct {
	outbox.PostSendCount
	ReportAlarmType         domain.AlarmType                       `json:"alarm_type"`
	ReportChannelID         string                                 `json:"channel_id"`
	ReportPostID            string                                 `json:"post_id"`
	ReportActualPublishedAt *time.Time                             `json:"actual_published_at,omitempty"`
	ReportAlarmSentAt       *time.Time                             `json:"alarm_sent_at,omitempty"`
	ReportDelaySeconds      *float64                               `json:"delay_seconds,omitempty"`
	PublishToDetectMillis   *int64                                 `json:"publish_to_detect_millis,omitempty"`
	DelaySource             outbox.PostDelaySource                 `json:"delay_source"`
	QueueWaitMillis         *int64                                 `json:"queue_wait_millis,omitempty"`
	RetryAccumulationMillis *int64                                 `json:"retry_accumulation_millis,omitempty"`
	JobFailureDetected      bool                                   `json:"job_failure_detected"`
	InternalDelayCause      outbox.PostInternalDelayCause          `json:"internal_delay_cause"`
	LatencyClassification   outbox.PostLatencyClassificationResult `json:"latency_classification"`
}

type sendCountKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

type rawRows struct {
	sendCountRows []outbox.PostSendCount
	timelineRows  []outbox.PostDeliveryTimeline
	query         Query
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (Report, error) {
	return CollectWithOptions(
		ctx,
		appConfig,
		logger,
		now,
		CollectOptions{Since: shared.CloneSendCountTime(&since)},
	)
}

func CollectWithOptions(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
) (Report, error) {
	if ctx == nil {
		return Report{}, fmt.Errorf("collect community shorts send count report: context is nil")
	}
	if appConfig == nil {
		return Report{}, fmt.Errorf("collect community shorts send count report: config is nil")
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
		return Report{}, fmt.Errorf("collect community shorts send count report: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts send count report: %w", err)
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
		return Report{}, fmt.Errorf("collect community shorts send count report: session is nil")
	}

	rows, err := collectRows(ctx, session, query, now)
	if err != nil {
		return Report{}, err
	}
	return BuildWithQuery(rows.sendCountRows, rows.timelineRows, rows.query, now), nil
}

func collectRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
) (rawRows, error) {
	switch query.Mode {
	case QueryModeObservation:
		return collectObservationRows(ctx, session, query, now)
	case QueryModeRecent:
		return collectRecentRows(ctx, session, query)
	default:
		return collectRecentRows(ctx, session, query)
	}
}

func collectObservationRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
) (rawRows, error) {
	state, err := shared.ResolveObservationQueryState(
		ctx,
		session.TrackingRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: find observation window: %w", err)
	}
	if state.Window == nil {
		return rawRows{}, fmt.Errorf(
			"collect community shorts send count report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}

	query.WindowStart = shared.CloneSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&state.EffectiveWindowEnd)
	if state.Finalized {
		return collectFinalizedObservationRows(ctx, session, query, state.Window.BigBangCutoverAt)
	}
	if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
		return collectActiveObservationRows(ctx, session, query, state)
	}
	return rawRows{query: query}, nil
}

func collectFinalizedObservationRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	cutoverAt time.Time,
) (rawRows, error) {
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func collectActiveObservationRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	state shared.ObservationQueryState,
) (rawRows, error) {
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list active observation-window send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list active observation-window delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func collectRecentRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
) (rawRows, error) {
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsSince(ctx, *query.WindowStart)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list post send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesSince(ctx, *query.WindowStart)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts send count report: list post delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func normalizeCollectOptions(
	options CollectOptions,
	now time.Time,
) (Query, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)

	if hasObservationQuery(observationRuntimeName, options.ObservationBigBangCutoverAt) {
		return normalizeObservationOptions(options, observationRuntimeName)
	}

	return normalizeRecentOptions(options, now)
}

func hasObservationQuery(runtimeName string, cutoverAt *time.Time) bool {
	return runtimeName != "" || cutoverAt != nil && !cutoverAt.IsZero()
}

func normalizeObservationOptions(
	options CollectOptions,
	observationRuntimeName string,
) (Query, error) {
	if options.Since != nil && !options.Since.IsZero() {
		return Query{}, fmt.Errorf("recent window and observation window are mutually exclusive")
	}
	if observationRuntimeName == "" || options.ObservationBigBangCutoverAt == nil || options.ObservationBigBangCutoverAt.IsZero() {
		return Query{}, fmt.Errorf("observation runtime name and cutover must both be set")
	}
	return Query{
		Mode:                        QueryModeObservation,
		ObservationRuntimeName:      observationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(options.ObservationBigBangCutoverAt),
	}, nil
}

func normalizeRecentOptions(
	options CollectOptions,
	now time.Time,
) (Query, error) {
	if options.Since == nil || options.Since.IsZero() {
		return Query{}, fmt.Errorf("recent window since is empty")
	}
	since := shared.NormalizeSendCountTime(*options.Since)
	if since.IsZero() {
		return Query{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return Query{}, fmt.Errorf("recent window since is after now")
	}

	return Query{
		Mode:        QueryModeRecent,
		WindowStart: shared.CloneSendCountTime(&since),
		WindowEnd:   shared.CloneSendCountTime(&now),
	}, nil
}

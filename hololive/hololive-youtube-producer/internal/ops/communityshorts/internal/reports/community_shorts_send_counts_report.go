package reports

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

type CommunityShortsSendCountQueryMode string

const (
	communityShortsSendCountQueryModeRecent      CommunityShortsSendCountQueryMode          = "recent_window"
	communityShortsSendCountQueryModeObservation CommunityShortsSendCountQueryMode          = "observation_window"
	communityShortsSendCountDuplicateAlarmPass   CommunityShortsSendCountVerificationStatus = "pass"
	communityShortsSendCountDuplicateAlarmFail   CommunityShortsSendCountVerificationStatus = "fail"
	communityShortsSendCountDuplicateAlarmRule                                              = "duplicate_success_posts == 0"
)

type CommunityShortsSendCountCollectOptions struct {
	Since                       *time.Time
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsSendCountQuery struct {
	Mode                        CommunityShortsSendCountQueryMode `json:"mode"`
	WindowStart                 *time.Time                        `json:"window_start,omitempty"`
	WindowEnd                   *time.Time                        `json:"window_end,omitempty"`
	ObservationRuntimeName      string                            `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time                        `json:"observation_bigbang_cutover_at,omitempty"`
}

type CommunityShortsSendCountReport struct {
	GeneratedAt  time.Time                            `json:"generated_at"`
	Query        CommunityShortsSendCountQuery        `json:"query"`
	WindowStart  time.Time                            `json:"window_start"`
	WindowEnd    time.Time                            `json:"window_end"`
	Summary      CommunityShortsSendCountSummary      `json:"summary"`
	Verification CommunityShortsSendCountVerification `json:"verification"`
	Rows         []CommunityShortsSendCountRow        `json:"rows"`
}

type CommunityShortsSendCountSummary struct {
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

type CommunityShortsSendCountVerificationStatus string

type CommunityShortsSendCountVerification struct {
	DuplicateAlarmStatus    CommunityShortsSendCountVerificationStatus `json:"duplicate_alarm_status"`
	DuplicateAlarmPostCount int                                        `json:"duplicate_alarm_post_count"`
	DuplicateAlarmRule      string                                     `json:"duplicate_alarm_rule"`
}

type CommunityShortsSendCountRow struct {
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

type communityShortsSendCountKey struct {
	channelID string
	alarmType domain.AlarmType
	contentID string
}

type communityShortsSendCountRows struct {
	sendCountRows []outbox.PostSendCount
	timelineRows  []outbox.PostDeliveryTimeline
	query         CommunityShortsSendCountQuery
}

func CollectCommunityShortsSendCountReport(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	since time.Time,
) (CommunityShortsSendCountReport, error) {
	return CollectCommunityShortsSendCountReportWithOptions(
		ctx,
		appConfig,
		logger,
		now,
		CommunityShortsSendCountCollectOptions{Since: cloneCommunityShortsSendCountTime(&since)},
	)
}

func CollectCommunityShortsSendCountReportWithOptions(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsSendCountCollectOptions,
) (CommunityShortsSendCountReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if appConfig == nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, err := normalizeCommunityShortsSendCountCollectOptions(options, now)
	if err != nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: %w", err)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, appConfig, logger)
	if err != nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectCommunityShortsSendCountReportWithSession(ctx, session, query, now)
}

func collectCommunityShortsSendCountReportWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsSendCountQuery,
	now time.Time,
) (CommunityShortsSendCountReport, error) {
	if session == nil {
		return CommunityShortsSendCountReport{}, fmt.Errorf("collect community shorts send count report: session is nil")
	}

	rows, err := collectCommunityShortsSendCountRows(ctx, session, query, now)
	if err != nil {
		return CommunityShortsSendCountReport{}, err
	}
	return BuildCommunityShortsSendCountReportWithQuery(rows.sendCountRows, rows.timelineRows, rows.query, now), nil
}

func collectCommunityShortsSendCountRows(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsSendCountQuery,
	now time.Time,
) (communityShortsSendCountRows, error) {
	switch query.Mode {
	case communityShortsSendCountQueryModeObservation:
		return collectObservationSendCountRows(ctx, session, query, now)
	default:
		return collectRecentSendCountRows(ctx, session, query)
	}
}

func collectObservationSendCountRows(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsSendCountQuery,
	now time.Time,
) (communityShortsSendCountRows, error) {
	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		session.trackingRepository,
		query.ObservationRuntimeName,
		*query.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: find observation window: %w", err)
	}
	if state.Window == nil {
		return communityShortsSendCountRows{}, fmt.Errorf(
			"collect community shorts send count report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}

	query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)
	if state.Finalized {
		return collectFinalizedObservationSendCountRows(ctx, session, query, state.Window.BigBangCutoverAt)
	}
	if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
		return collectActiveObservationSendCountRows(ctx, session, query, state)
	}
	return communityShortsSendCountRows{query: query}, nil
}

func collectFinalizedObservationSendCountRows(ctx context.Context, session *communityShortsOpsSession, query CommunityShortsSendCountQuery, cutoverAt time.Time) (communityShortsSendCountRows, error) {
	sendCountRows, err := session.telemetryRepository.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window send counts: %w", err)
	}
	timelineRows, err := session.telemetryRepository.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list finalized observation-window delivery timelines: %w", err)
	}
	return communityShortsSendCountRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func collectActiveObservationSendCountRows(ctx context.Context, session *communityShortsOpsSession, query CommunityShortsSendCountQuery, state communityShortsObservationQueryState) (communityShortsSendCountRows, error) {
	sendCountRows, err := session.telemetryRepository.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list active observation-window send counts: %w", err)
	}
	timelineRows, err := session.telemetryRepository.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list active observation-window delivery timelines: %w", err)
	}
	return communityShortsSendCountRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func collectRecentSendCountRows(ctx context.Context, session *communityShortsOpsSession, query CommunityShortsSendCountQuery) (communityShortsSendCountRows, error) {
	sendCountRows, err := session.telemetryRepository.ListPostSendCountsSince(ctx, *query.WindowStart)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list post send counts: %w", err)
	}
	timelineRows, err := session.telemetryRepository.ListPostDeliveryTimelinesSince(ctx, *query.WindowStart)
	if err != nil {
		return communityShortsSendCountRows{}, fmt.Errorf("collect community shorts send count report: list post delivery timelines: %w", err)
	}
	return communityShortsSendCountRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query}, nil
}

func normalizeCommunityShortsSendCountCollectOptions(
	options CommunityShortsSendCountCollectOptions,
	now time.Time,
) (CommunityShortsSendCountQuery, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)

	if hasCommunityShortsSendCountObservationQuery(observationRuntimeName, options.ObservationBigBangCutoverAt) {
		return normalizeCommunityShortsSendCountObservationOptions(options, observationRuntimeName)
	}

	return normalizeCommunityShortsSendCountRecentOptions(options, now)
}

func hasCommunityShortsSendCountObservationQuery(runtimeName string, cutoverAt *time.Time) bool {
	return runtimeName != "" || cutoverAt != nil && !cutoverAt.IsZero()
}

func normalizeCommunityShortsSendCountObservationOptions(
	options CommunityShortsSendCountCollectOptions,
	observationRuntimeName string,
) (CommunityShortsSendCountQuery, error) {
	if options.Since != nil && !options.Since.IsZero() {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window and observation window are mutually exclusive")
	}
	if observationRuntimeName == "" || options.ObservationBigBangCutoverAt == nil || options.ObservationBigBangCutoverAt.IsZero() {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("observation runtime name and cutover must both be set")
	}
	return CommunityShortsSendCountQuery{
		Mode:                        communityShortsSendCountQueryModeObservation,
		ObservationRuntimeName:      observationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
	}, nil
}

func normalizeCommunityShortsSendCountRecentOptions(
	options CommunityShortsSendCountCollectOptions,
	now time.Time,
) (CommunityShortsSendCountQuery, error) {
	if options.Since == nil || options.Since.IsZero() {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is empty")
	}
	since := normalizeCommunityShortsSendCountTime(*options.Since)
	if since.IsZero() {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is empty")
	}
	if since.After(now) {
		return CommunityShortsSendCountQuery{}, fmt.Errorf("recent window since is after now")
	}

	return CommunityShortsSendCountQuery{
		Mode:        communityShortsSendCountQueryModeRecent,
		WindowStart: cloneCommunityShortsSendCountTime(&since),
		WindowEnd:   cloneCommunityShortsSendCountTime(&now),
	}, nil
}

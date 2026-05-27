package latencycause

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

const (
	observedAtBasis        = "COALESCE(actual_published_at, detected_at)"
	observationPeriodLabel = "observation_window"
	internalCauseRule      = "internal_system if delay_source in {internal_delivery,mixed} OR (internal_delay_cause != none AND delay_source != external_collection)"
	nonInternalCauseRule   = "non_internal if delay_source = external_collection OR (delay_source = none AND internal_delay_cause = none)"
	excludedExternalRule   = "delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts"
	insufficientEvidence   = "latency_classification.status = insufficient_evidence keeps the row in non_internal and increments insufficient_evidence_posts"
)

var evidenceFieldCatalog = []string{
	"delay_source",
	"internal_delay_cause",
	"alarm_latency_millis",
	"publish_to_detect_millis",
	"internal_latency_millis",
	"queue_wait_millis",
	"retry_accumulation_millis",
	"job_failure_detected",
	"latency_classification.status",
	"latency_classification.evidence",
}

type QueryMode string

const (
	queryModeRecent      QueryMode = "recent_window"
	queryModeObservation QueryMode = "observation_window"
)

type InternalCauseJudgment string

const (
	InternalCauseJudgmentInternalSystem InternalCauseJudgment = "internal_system"
	InternalCauseJudgmentNonInternal    InternalCauseJudgment = "non_internal"
)

type CollectOptions struct {
	PeriodSpecs                 []PeriodSpec
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

type Verification struct {
	InternalCauseRule        string   `json:"internal_cause_rule"`
	NonInternalCauseRule     string   `json:"non_internal_cause_rule"`
	ExcludedExternalRule     string   `json:"excluded_external_rule"`
	InsufficientEvidenceRule string   `json:"insufficient_evidence_rule"`
	EvidenceFieldCatalog     []string `json:"evidence_field_catalog"`
}

type Evidence struct {
	Fields                     []string                                      `json:"fields"`
	SelectedClassificationKeys []outbox.PostLatencyClassificationEvidenceKey `json:"selected_classification_keys,omitempty"`
}

type Report struct {
	GeneratedAt      time.Time    `json:"generated_at"`
	Query            Query        `json:"query"`
	ObservedAtBasis  string       `json:"observed_at_basis"`
	ThresholdMillis  int64        `json:"threshold_millis"`
	Verification     Verification `json:"verification"`
	RequestedPeriods []PeriodSpec `json:"requested_periods"`
	Periods          []PeriodView `json:"periods"`
}

type PeriodView struct {
	Summary      outbox.PostLatencyPeriodSummary `json:"summary"`
	CauseSummary Summary                         `json:"cause_summary"`
	Rows         []Row                           `json:"rows"`
}

type Summary struct {
	ExceededPostCount                  int64 `json:"exceeded_post_count"`
	InternalSystemCausePostCount       int64 `json:"internal_system_cause_post_count"`
	NonInternalSystemCausePostCount    int64 `json:"non_internal_system_cause_post_count"`
	ExcludedExternalDelayPostCount     int64 `json:"excluded_external_delay_post_count"`
	CommunityExceededPostCount         int64 `json:"community_exceeded_post_count"`
	ShortsExceededPostCount            int64 `json:"shorts_exceeded_post_count"`
	ExternalCollectionSourcePostCount  int64 `json:"external_collection_source_post_count"`
	InternalDeliverySourcePostCount    int64 `json:"internal_delivery_source_post_count"`
	MixedDelaySourcePostCount          int64 `json:"mixed_delay_source_post_count"`
	NoDominantSourcePostCount          int64 `json:"no_dominant_source_post_count"`
	InternalCauseCandidatePostCount    int64 `json:"internal_cause_candidate_post_count"`
	QueueWaitCausePostCount            int64 `json:"queue_wait_cause_post_count"`
	RetryAccumulationCausePostCount    int64 `json:"retry_accumulation_cause_post_count"`
	JobFailureCausePostCount           int64 `json:"job_failure_cause_post_count"`
	UnclassifiedInternalCausePostCount int64 `json:"unclassified_internal_cause_post_count"`
	InsufficientEvidencePostCount      int64 `json:"insufficient_evidence_post_count"`
}

type Row struct {
	AlarmType               domain.AlarmType                       `json:"alarm_type"`
	ChannelID               string                                 `json:"channel_id"`
	PostID                  string                                 `json:"post_id"`
	ContentID               string                                 `json:"content_id"`
	ObservedAt              *time.Time                             `json:"observed_at,omitempty"`
	ActualPublishedAt       *time.Time                             `json:"actual_published_at,omitempty"`
	DetectedAt              *time.Time                             `json:"detected_at,omitempty"`
	AlarmSentAt             *time.Time                             `json:"alarm_sent_at,omitempty"`
	AlarmLatencyMillis      *int64                                 `json:"alarm_latency_millis,omitempty"`
	PublishToDetectMillis   *int64                                 `json:"publish_to_detect_millis,omitempty"`
	InternalLatencyMillis   *int64                                 `json:"internal_latency_millis,omitempty"`
	QueueWaitMillis         *int64                                 `json:"queue_wait_millis,omitempty"`
	RetryAccumulationMillis *int64                                 `json:"retry_accumulation_millis,omitempty"`
	JobFailureDetected      bool                                   `json:"job_failure_detected"`
	DelaySource             outbox.PostDelaySource                 `json:"delay_source"`
	InternalDelayCause      outbox.PostInternalDelayCause          `json:"internal_delay_cause"`
	InternalCauseJudgment   InternalCauseJudgment                  `json:"internal_cause_judgment"`
	InternalCauseBasis      string                                 `json:"internal_cause_basis"`
	CauseEvidence           Evidence                               `json:"cause_evidence"`
	LatencyClassification   outbox.PostLatencyClassificationResult `json:"latency_classification"`
}

type rawRows struct {
	sendCountRows []outbox.PostSendCount
	timelineRows  []outbox.PostDeliveryTimeline
	query         Query
	periods       []outbox.PostLatencyPeriod
}

func Collect(
	ctx context.Context,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	specs []PeriodSpec,
) (Report, error) {
	return CollectWithOptions(
		ctx,
		appConfig,
		logger,
		now,
		CollectOptions{PeriodSpecs: specs},
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
		ctx = context.Background()
	}
	if appConfig == nil {
		return Report{}, fmt.Errorf("collect community shorts latency cause report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, periods, err := normalizeCollectOptions(options, now)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectWithSession(ctx, session, query, now, periods)
}

func collectWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
	periods []outbox.PostLatencyPeriod,
) (Report, error) {
	if session == nil {
		return Report{}, fmt.Errorf("collect community shorts latency cause report: session is nil")
	}

	rows, err := collectRows(ctx, session, query, now, periods)
	if err != nil {
		return Report{}, err
	}
	report, err := BuildWithQuery(rows.sendCountRows, rows.timelineRows, rows.query, now, rows.periods)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}
	return report, nil
}

func collectRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	now time.Time,
	periods []outbox.PostLatencyPeriod,
) (rawRows, error) {
	switch query.Mode {
	case queryModeObservation:
		return collectObservationRows(ctx, session, query, now)
	default:
		return collectRecentRows(ctx, session, query, periods)
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
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: find observation window: %w", err)
	}
	if state.Window == nil {
		return rawRows{}, fmt.Errorf(
			"collect community shorts latency cause report: observation window not found: runtime=%s cutover=%s",
			query.ObservationRuntimeName,
			shared.FormatSendCountTime(*query.ObservationBigBangCutoverAt),
		)
	}

	query, periods := withObservationWindow(query, state)
	if state.Finalized {
		return collectFinalizedObservationRows(ctx, session, query, periods, state.Window.BigBangCutoverAt)
	}
	if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
		return collectActiveObservationRows(ctx, session, query, periods, state)
	}
	return rawRows{query: query, periods: periods}, nil
}

func withObservationWindow(query Query, state shared.ObservationQueryState) (Query, []outbox.PostLatencyPeriod) {
	query.WindowStart = shared.CloneSendCountTime(&state.Window.ObservationStartedAt)
	query.WindowEnd = shared.CloneSendCountTime(&state.EffectiveWindowEnd)
	if !state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
		return query, nil
	}
	return query, []outbox.PostLatencyPeriod{{
		Label:   observationPeriodLabel,
		StartAt: state.Window.ObservationStartedAt,
		EndAt:   state.EffectiveWindowEnd,
	}}
}

func collectFinalizedObservationRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	periods []outbox.PostLatencyPeriod,
	cutoverAt time.Time,
) (rawRows, error) {
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, cutoverAt)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query, periods: periods}, nil
}

func collectActiveObservationRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	periods []outbox.PostLatencyPeriod,
	state shared.ObservationQueryState,
) (rawRows, error) {
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query, periods: periods}, nil
}

func collectRecentRows(
	ctx context.Context,
	session *shared.OpsSession,
	query Query,
	periods []outbox.PostLatencyPeriod,
) (rawRows, error) {
	since := earliestPeriodStart(periods)
	sendCountRows, err := session.TelemetryRepository.ListPostSendCountsSince(ctx, since)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list post send counts: %w", err)
	}
	timelineRows, err := session.TelemetryRepository.ListPostDeliveryTimelinesSince(ctx, since)
	if err != nil {
		return rawRows{}, fmt.Errorf("collect community shorts latency cause report: list post delivery timelines: %w", err)
	}
	return rawRows{sendCountRows: sendCountRows, timelineRows: timelineRows, query: query, periods: periods}, nil
}

func normalizeCollectOptions(
	options CollectOptions,
	now time.Time,
) (Query, []outbox.PostLatencyPeriod, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)

	if hasObservationQuery(observationRuntimeName, options.ObservationBigBangCutoverAt) {
		return normalizeObservationOptions(options, observationRuntimeName)
	}

	periods, err := buildPeriods(now, options.PeriodSpecs)
	if err != nil {
		return Query{}, nil, err
	}

	return withQueryWindow(Query{Mode: queryModeRecent}, periods), periods, nil
}

func hasObservationQuery(runtimeName string, cutoverAt *time.Time) bool {
	return runtimeName != "" || cutoverAt != nil && !cutoverAt.IsZero()
}

func normalizeObservationOptions(
	options CollectOptions,
	observationRuntimeName string,
) (Query, []outbox.PostLatencyPeriod, error) {
	if len(options.PeriodSpecs) > 0 {
		return Query{}, nil, fmt.Errorf("period specs and observation window are mutually exclusive")
	}
	if observationRuntimeName == "" || options.ObservationBigBangCutoverAt == nil || options.ObservationBigBangCutoverAt.IsZero() {
		return Query{}, nil, fmt.Errorf("observation runtime name and cutover must both be set")
	}
	return Query{
		Mode:                        queryModeObservation,
		ObservationRuntimeName:      observationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(options.ObservationBigBangCutoverAt),
	}, nil, nil
}

func earliestPeriodStart(periods []outbox.PostLatencyPeriod) time.Time {
	if len(periods) == 0 {
		return time.Time{}
	}
	startAt := periods[0].StartAt
	for i := 1; i < len(periods); i++ {
		if periods[i].StartAt.Before(startAt) {
			startAt = periods[i].StartAt
		}
	}
	return startAt.UTC()
}

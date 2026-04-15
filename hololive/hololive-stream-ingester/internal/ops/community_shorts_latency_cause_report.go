package ops

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

const (
	communityShortsLatencyCauseObservedAtBasis        = "COALESCE(actual_published_at, detected_at)"
	communityShortsLatencyCauseObservationPeriodLabel = "observation_window"
	communityShortsLatencyCauseInternalCauseRule      = "internal_system if delay_source in {internal_delivery,mixed} OR (internal_delay_cause != none AND delay_source != external_collection)"
	communityShortsLatencyCauseNonInternalCauseRule   = "non_internal if delay_source = external_collection OR (delay_source = none AND internal_delay_cause = none)"
	communityShortsLatencyCauseExcludedExternalRule   = "delay_source = external_collection rows stay logged as reference-only excluded_external_delay_posts and do not contribute to failure-driving counts"
	communityShortsLatencyCauseInsufficientEvidence   = "latency_classification.status = insufficient_evidence keeps the row in non_internal and increments insufficient_evidence_posts"
)

var communityShortsLatencyCauseEvidenceFieldCatalog = []string{
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

type CommunityShortsLatencyCauseQueryMode string

const (
	communityShortsLatencyCauseQueryModeRecent      CommunityShortsLatencyCauseQueryMode = "recent_window"
	communityShortsLatencyCauseQueryModeObservation CommunityShortsLatencyCauseQueryMode = "observation_window"
)

type CommunityShortsInternalCauseJudgment string

const (
	CommunityShortsInternalCauseJudgmentInternalSystem CommunityShortsInternalCauseJudgment = "internal_system"
	CommunityShortsInternalCauseJudgmentNonInternal    CommunityShortsInternalCauseJudgment = "non_internal"
)

type CommunityShortsLatencyCauseCollectOptions struct {
	PeriodSpecs                 []CommunityShortsLatencyPeriodSpec
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time
}

type CommunityShortsLatencyCauseQuery struct {
	Mode                        CommunityShortsLatencyCauseQueryMode `json:"mode"`
	WindowStart                 *time.Time                           `json:"window_start,omitempty"`
	WindowEnd                   *time.Time                           `json:"window_end,omitempty"`
	ObservationRuntimeName      string                               `json:"observation_runtime_name,omitempty"`
	ObservationBigBangCutoverAt *time.Time                           `json:"observation_bigbang_cutover_at,omitempty"`
}

type CommunityShortsLatencyCauseVerification struct {
	InternalCauseRule        string   `json:"internal_cause_rule"`
	NonInternalCauseRule     string   `json:"non_internal_cause_rule"`
	ExcludedExternalRule     string   `json:"excluded_external_rule"`
	InsufficientEvidenceRule string   `json:"insufficient_evidence_rule"`
	EvidenceFieldCatalog     []string `json:"evidence_field_catalog"`
}

type CommunityShortsLatencyCauseEvidence struct {
	Fields                     []string                                      `json:"fields"`
	SelectedClassificationKeys []outbox.PostLatencyClassificationEvidenceKey `json:"selected_classification_keys,omitempty"`
}

type CommunityShortsLatencyCauseReport struct {
	GeneratedAt      time.Time                               `json:"generated_at"`
	Query            CommunityShortsLatencyCauseQuery        `json:"query"`
	ObservedAtBasis  string                                  `json:"observed_at_basis"`
	ThresholdMillis  int64                                   `json:"threshold_millis"`
	Verification     CommunityShortsLatencyCauseVerification `json:"verification"`
	RequestedPeriods []CommunityShortsLatencyPeriodSpec      `json:"requested_periods"`
	Periods          []CommunityShortsLatencyCausePeriodView `json:"periods"`
}

type CommunityShortsLatencyCausePeriodView struct {
	Summary      outbox.PostLatencyPeriodSummary    `json:"summary"`
	CauseSummary CommunityShortsLatencyCauseSummary `json:"cause_summary"`
	Rows         []CommunityShortsLatencyCauseRow   `json:"rows"`
}

type CommunityShortsLatencyCauseSummary struct {
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

type CommunityShortsLatencyCauseRow struct {
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
	InternalCauseJudgment   CommunityShortsInternalCauseJudgment   `json:"internal_cause_judgment"`
	InternalCauseBasis      string                                 `json:"internal_cause_basis"`
	CauseEvidence           CommunityShortsLatencyCauseEvidence    `json:"cause_evidence"`
	LatencyClassification   outbox.PostLatencyClassificationResult `json:"latency_classification"`
}

func CollectCommunityShortsLatencyCauseReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	specs []CommunityShortsLatencyPeriodSpec,
) (CommunityShortsLatencyCauseReport, error) {
	return CollectCommunityShortsLatencyCauseReportWithOptions(
		ctx,
		cfg,
		logger,
		now,
		CommunityShortsLatencyCauseCollectOptions{PeriodSpecs: specs},
	)
}

func CollectCommunityShortsLatencyCauseReportWithOptions(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsLatencyCauseCollectOptions,
) (CommunityShortsLatencyCauseReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}

	query, periods, err := normalizeCommunityShortsLatencyCauseCollectOptions(options, now)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectCommunityShortsLatencyCauseReportWithSession(ctx, session, query, now, periods)
}

func collectCommunityShortsLatencyCauseReportWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	query CommunityShortsLatencyCauseQuery,
	now time.Time,
	periods []outbox.PostLatencyPeriod,
) (CommunityShortsLatencyCauseReport, error) {
	if session == nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: session is nil")
	}

	var err error
	var sendCountRows []outbox.PostSendCount
	var timelineRows []outbox.PostDeliveryTimeline
	switch query.Mode {
	case communityShortsLatencyCauseQueryModeObservation:
		state, stateErr := resolveCommunityShortsObservationQueryState(
			ctx,
			session.trackingRepository,
			query.ObservationRuntimeName,
			*query.ObservationBigBangCutoverAt,
			now,
		)
		if stateErr != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: find observation window: %w", stateErr)
		}
		if state.Window == nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf(
				"collect community shorts latency cause report: observation window not found: runtime=%s cutover=%s",
				query.ObservationRuntimeName,
				formatCommunityShortsSendCountTime(*query.ObservationBigBangCutoverAt),
			)
		}
		query.WindowStart = cloneCommunityShortsSendCountTime(&state.Window.ObservationStartedAt)
		query.WindowEnd = cloneCommunityShortsSendCountTime(&state.EffectiveWindowEnd)
		if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
			periods = []outbox.PostLatencyPeriod{{
				Label:   communityShortsLatencyCauseObservationPeriodLabel,
				StartAt: state.Window.ObservationStartedAt,
				EndAt:   state.EffectiveWindowEnd,
			}}
		} else {
			periods = nil
		}

		if state.Finalized {
			sendCountRows, err = session.telemetryRepo.ListPostSendCountsByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window send counts: %w", err)
			}
			timelineRows, err = session.telemetryRepo.ListPostDeliveryTimelinesByFinalizedObservationWindow(ctx, query.ObservationRuntimeName, state.Window.BigBangCutoverAt)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list finalized observation-window delivery timelines: %w", err)
			}
			break
		}

		if state.EffectiveWindowEnd.After(state.Window.ObservationStartedAt) {
			sendCountRows, err = session.telemetryRepo.ListPostSendCountsWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window send counts: %w", err)
			}
			timelineRows, err = session.telemetryRepo.ListPostDeliveryTimelinesWithinObservationWindow(ctx, state.Window.ObservationStartedAt, state.EffectiveWindowEnd, state.EffectiveWindowEnd)
			if err != nil {
				return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list active observation-window delivery timelines: %w", err)
			}
		}
	default:
		since := earliestCommunityShortsLatencyCausePeriodStart(periods)
		sendCountRows, err = session.telemetryRepo.ListPostSendCountsSince(ctx, since)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list post send counts: %w", err)
		}
		timelineRows, err = session.telemetryRepo.ListPostDeliveryTimelinesSince(ctx, since)
		if err != nil {
			return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: list post delivery timelines: %w", err)
		}
	}

	report, err := BuildCommunityShortsLatencyCauseReportWithQuery(sendCountRows, timelineRows, query, now, periods)
	if err != nil {
		return CommunityShortsLatencyCauseReport{}, fmt.Errorf("collect community shorts latency cause report: %w", err)
	}
	return report, nil
}

func normalizeCommunityShortsLatencyCauseCollectOptions(
	options CommunityShortsLatencyCauseCollectOptions,
	now time.Time,
) (CommunityShortsLatencyCauseQuery, []outbox.PostLatencyPeriod, error) {
	observationRuntimeName := strings.TrimSpace(options.ObservationRuntimeName)
	hasObservationCutover := options.ObservationBigBangCutoverAt != nil && !options.ObservationBigBangCutoverAt.IsZero()
	hasObservationQuery := observationRuntimeName != "" || hasObservationCutover

	if hasObservationQuery {
		if len(options.PeriodSpecs) > 0 {
			return CommunityShortsLatencyCauseQuery{}, nil, fmt.Errorf("period specs and observation window are mutually exclusive")
		}
		if observationRuntimeName == "" || !hasObservationCutover {
			return CommunityShortsLatencyCauseQuery{}, nil, fmt.Errorf("observation runtime name and cutover must both be set")
		}
		return CommunityShortsLatencyCauseQuery{
			Mode:                        communityShortsLatencyCauseQueryModeObservation,
			ObservationRuntimeName:      observationRuntimeName,
			ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
		}, nil, nil
	}

	periods, err := buildCommunityShortsLatencyPeriods(now, options.PeriodSpecs)
	if err != nil {
		return CommunityShortsLatencyCauseQuery{}, nil, err
	}

	return withCommunityShortsLatencyCauseQueryWindow(CommunityShortsLatencyCauseQuery{Mode: communityShortsLatencyCauseQueryModeRecent}, periods), periods, nil
}

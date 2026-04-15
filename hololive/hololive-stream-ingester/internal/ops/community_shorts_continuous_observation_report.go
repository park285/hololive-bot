package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

type CommunityShortsContinuousObservationStatus string

const (
	CommunityShortsContinuousObservationStatusActive    CommunityShortsContinuousObservationStatus = "active"
	CommunityShortsContinuousObservationStatusFinalized CommunityShortsContinuousObservationStatus = "finalized"
	communityShortsContinuousObservationDefaultLogLimit                                            = 200
)

type CommunityShortsContinuousObservationCollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt time.Time
	DeliveryLogLimit            int
	LatencyPeriodSpecs          []CommunityShortsLatencyPeriodSpec
}

type CommunityShortsContinuousObservationCloseoutStatus string

const (
	CommunityShortsContinuousObservationCloseoutStatusPending              CommunityShortsContinuousObservationCloseoutStatus = "pending"
	CommunityShortsContinuousObservationCloseoutStatusPass                 CommunityShortsContinuousObservationCloseoutStatus = "pass"
	CommunityShortsContinuousObservationCloseoutStatusFail                 CommunityShortsContinuousObservationCloseoutStatus = "fail"
	CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence CommunityShortsContinuousObservationCloseoutStatus = "insufficient_evidence"
)

type CommunityShortsContinuousObservationWindow struct {
	RuntimeName           string                                     `json:"runtime_name"`
	BigBangCutoverAt      time.Time                                  `json:"bigbang_cutover_at"`
	AppVersion            string                                     `json:"app_version"`
	TargetChannelCount    int                                        `json:"target_channel_count"`
	DeploymentCompletedAt time.Time                                  `json:"deployment_completed_at"`
	ObservationStartedAt  time.Time                                  `json:"observation_started_at"`
	ObservationEndsAt     time.Time                                  `json:"observation_ends_at"`
	ObservedUntil         time.Time                                  `json:"observed_until"`
	Status                CommunityShortsContinuousObservationStatus `json:"status"`
}

type CommunityShortsContinuousObservation24HCloseout struct {
	Status                            CommunityShortsContinuousObservationCloseoutStatus `json:"status"`
	AggregationScope                  string                                             `json:"aggregation_scope"`
	TargetChannelCount                int                                                `json:"target_channel_count"`
	ObservedPostCount                 int                                                `json:"observed_post_count"`
	ObservationPeriodLabel            string                                             `json:"observation_period_label"`
	TotalExceededPostCount            int64                                              `json:"total_exceeded_post_count"`
	InternalExceededPostCount         int64                                              `json:"internal_exceeded_post_count"`
	NonInternalExceededPostCount      int64                                              `json:"non_internal_exceeded_post_count"`
	ExcludedExternalExceededPostCount int64                                              `json:"excluded_external_exceeded_post_count"`
	Rule                              string                                             `json:"rule"`
	Statement                         string                                             `json:"statement"`
}

type CommunityShortsContinuousObservationMissingAlarmCloseout struct {
	Status                    CommunityShortsContinuousObservationCloseoutStatus `json:"status"`
	AggregationScope          string                                             `json:"aggregation_scope"`
	TargetChannelCount        int                                                `json:"target_channel_count"`
	ReferencePostCount        int                                                `json:"reference_post_count"`
	SendStatePostCount        int                                                `json:"send_state_post_count"`
	MissingAlarmPostCount     int                                                `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int                                                `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int                                                `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int                                                `json:"not_sent_missing_post_count"`
	Rule                      string                                             `json:"rule"`
	Statement                 string                                             `json:"statement"`
}

type CommunityShortsContinuousObservationStateConsistencyCloseout struct {
	Status                    CommunityShortsContinuousObservationCloseoutStatus `json:"status"`
	AggregationScope          string                                             `json:"aggregation_scope"`
	TargetChannelCount        int                                                `json:"target_channel_count"`
	ReferencePostCount        int                                                `json:"reference_post_count"`
	SendStatePostCount        int                                                `json:"send_state_post_count"`
	DuplicateSentPostCount    int                                                `json:"duplicate_sent_post_count"`
	MissingAlarmPostCount     int                                                `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int                                                `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int                                                `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int                                                `json:"not_sent_missing_post_count"`
	Rule                      string                                             `json:"rule"`
	Statement                 string                                             `json:"statement"`
}

type CommunityShortsContinuousObservationReport struct {
	GeneratedAt                 time.Time                                                    `json:"generated_at"`
	Observation                 CommunityShortsContinuousObservationWindow                   `json:"observation"`
	Closeout24H                 CommunityShortsContinuousObservation24HCloseout              `json:"closeout_24h"`
	MissingAlarmCloseout24H     CommunityShortsContinuousObservationMissingAlarmCloseout     `json:"missing_alarm_closeout_24h"`
	StateConsistencyCloseout24H CommunityShortsContinuousObservationStateConsistencyCloseout `json:"state_consistency_closeout_24h"`
	TargetBaseline              communityshorts.TargetBaseline                               `json:"target_baseline"`
	ChannelSummary              CommunityShortsChannelSummaryReport                          `json:"channel_summary"`
	SendCounts                  CommunityShortsSendCountReport                               `json:"send_counts"`
	AlarmSentHistoryDataset     *CommunityShortsAlarmSentHistoryDatasetReport                `json:"alarm_sent_history_dataset,omitempty"`
	DeliveryLogs                CommunityShortsDeliveryLogReport                             `json:"delivery_logs"`
	LatencyPeriods              CommunityShortsLatencyPeriodReport                           `json:"latency_periods"`
	LatencyCause                CommunityShortsLatencyCauseReport                            `json:"latency_cause"`
}

func DefaultCommunityShortsContinuousObservationPeriodSpecs() []CommunityShortsLatencyPeriodSpec {
	return []CommunityShortsLatencyPeriodSpec{
		{Label: "last_15m", Window: 15 * time.Minute},
		{Label: "last_1h", Window: time.Hour},
		{Label: "last_24h", Window: 24 * time.Hour},
	}
}

func CollectCommunityShortsContinuousObservationReport(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
) (CommunityShortsContinuousObservationReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = normalizeCommunityShortsSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	options = normalizeCommunityShortsContinuousObservationCollectOptions(options)
	if err := validateCommunityShortsContinuousObservationCollectOptions(options); err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}

	session, cleanupDB, err := openCommunityShortsOpsSession(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectCommunityShortsContinuousObservationReportWithSession(
		ctx,
		session,
		cfg,
		logger,
		now,
		options,
		defaultCommunityShortsContinuousObservationCollectorWiring(),
	)
}

func collectCommunityShortsContinuousObservationReportWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (CommunityShortsContinuousObservationReport, error) {
	artifacts, err := collectCommunityShortsContinuousObservationArtifacts(
		ctx,
		session,
		cfg,
		logger,
		now,
		options,
		wiring,
	)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}

	return buildCommunityShortsContinuousObservationReport(now, artifacts), nil
}

func buildCommunityShortsContinuousObservationReport(
	now time.Time,
	artifacts communityShortsContinuousObservationArtifacts,
) CommunityShortsContinuousObservationReport {
	return CommunityShortsContinuousObservationReport{
		GeneratedAt:                 now,
		Observation:                 artifacts.Observation,
		Closeout24H:                 buildCommunityShortsContinuousObservation24HCloseout(artifacts.Observation, artifacts.TargetBaseline, artifacts.SendCounts, artifacts.LatencyCause),
		MissingAlarmCloseout24H:     buildCommunityShortsContinuousObservationMissingAlarmCloseout(artifacts.Observation, artifacts.TargetBaseline, artifacts.AlarmSentHistoryDataset, artifacts.AlarmSentHistoryDatasetErr),
		StateConsistencyCloseout24H: buildCommunityShortsContinuousObservationStateConsistencyCloseout(artifacts.Observation, artifacts.TargetBaseline, artifacts.AlarmSentHistoryDataset, artifacts.AlarmSentHistoryDatasetErr),
		TargetBaseline:              artifacts.TargetBaseline,
		ChannelSummary:              artifacts.ChannelSummary,
		SendCounts:                  artifacts.SendCounts,
		AlarmSentHistoryDataset:     artifacts.AlarmSentHistoryDataset,
		DeliveryLogs:                artifacts.DeliveryLogs,
		LatencyPeriods:              artifacts.LatencyPeriods,
		LatencyCause:                artifacts.LatencyCause,
	}
}

func normalizeCommunityShortsContinuousObservationCollectOptions(
	options CommunityShortsContinuousObservationCollectOptions,
) CommunityShortsContinuousObservationCollectOptions {
	options.ObservationRuntimeName = strings.TrimSpace(options.ObservationRuntimeName)
	options.ObservationBigBangCutoverAt = normalizeCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt)
	if options.DeliveryLogLimit <= 0 {
		options.DeliveryLogLimit = communityShortsContinuousObservationDefaultLogLimit
	}
	if len(options.LatencyPeriodSpecs) == 0 {
		options.LatencyPeriodSpecs = DefaultCommunityShortsContinuousObservationPeriodSpecs()
	}
	return options
}

func validateCommunityShortsContinuousObservationCollectOptions(
	options CommunityShortsContinuousObservationCollectOptions,
) error {
	if options.ObservationRuntimeName == "" {
		return fmt.Errorf("observation runtime name is empty")
	}
	if options.ObservationBigBangCutoverAt.IsZero() {
		return fmt.Errorf("observation big-bang cutover at is empty")
	}
	if options.DeliveryLogLimit <= 0 {
		return fmt.Errorf("delivery log limit must be greater than zero")
	}
	return nil
}

package continuousobservation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/channelsummary"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/deliverylogs"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusFinalized Status = "finalized"
	defaultLogLimit        = 200
)

type CollectOptions struct {
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt time.Time
	DeliveryLogLimit            int
	LatencyPeriodSpecs          []latencycause.PeriodSpec
}

type CloseoutStatus string

const (
	CloseoutStatusPending              CloseoutStatus = "pending"
	CloseoutStatusPass                 CloseoutStatus = "pass"
	CloseoutStatusFail                 CloseoutStatus = "fail"
	CloseoutStatusInsufficientEvidence CloseoutStatus = "insufficient_evidence"
)

type Window struct {
	RuntimeName           string    `json:"runtime_name"`
	BigBangCutoverAt      time.Time `json:"bigbang_cutover_at"`
	AppVersion            string    `json:"app_version"`
	TargetChannelCount    int       `json:"target_channel_count"`
	DeploymentCompletedAt time.Time `json:"deployment_completed_at"`
	ObservationStartedAt  time.Time `json:"observation_started_at"`
	ObservationEndsAt     time.Time `json:"observation_ends_at"`
	ObservedUntil         time.Time `json:"observed_until"`
	Status                Status    `json:"status"`
}

type Closeout24H struct {
	Status                            CloseoutStatus `json:"status"`
	AggregationScope                  string         `json:"aggregation_scope"`
	TargetChannelCount                int            `json:"target_channel_count"`
	ObservedPostCount                 int            `json:"observed_post_count"`
	ObservationPeriodLabel            string         `json:"observation_period_label"`
	TotalExceededPostCount            int64          `json:"total_exceeded_post_count"`
	InternalExceededPostCount         int64          `json:"internal_exceeded_post_count"`
	NonInternalExceededPostCount      int64          `json:"non_internal_exceeded_post_count"`
	ExcludedExternalExceededPostCount int64          `json:"excluded_external_exceeded_post_count"`
	Rule                              string         `json:"rule"`
	Statement                         string         `json:"statement"`
}

type MissingAlarmCloseout struct {
	Status                    CloseoutStatus `json:"status"`
	AggregationScope          string         `json:"aggregation_scope"`
	TargetChannelCount        int            `json:"target_channel_count"`
	ReferencePostCount        int            `json:"reference_post_count"`
	SendStatePostCount        int            `json:"send_state_post_count"`
	MissingAlarmPostCount     int            `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int            `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int            `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int            `json:"not_sent_missing_post_count"`
	Rule                      string         `json:"rule"`
	Statement                 string         `json:"statement"`
}

type StateConsistencyCloseout struct {
	Status                    CloseoutStatus `json:"status"`
	AggregationScope          string         `json:"aggregation_scope"`
	TargetChannelCount        int            `json:"target_channel_count"`
	ReferencePostCount        int            `json:"reference_post_count"`
	SendStatePostCount        int            `json:"send_state_post_count"`
	DuplicateSentPostCount    int            `json:"duplicate_sent_post_count"`
	MissingAlarmPostCount     int            `json:"missing_alarm_post_count"`
	MissingSendStatePostCount int            `json:"missing_send_state_post_count"`
	AttemptedMissingPostCount int            `json:"attempted_missing_post_count"`
	NotSentMissingPostCount   int            `json:"not_sent_missing_post_count"`
	Rule                      string         `json:"rule"`
	Statement                 string         `json:"statement"`
}

type Report struct {
	GeneratedAt                 time.Time                      `json:"generated_at"`
	Observation                 Window                         `json:"observation"`
	Closeout24H                 Closeout24H                    `json:"closeout_24h"`
	MissingAlarmCloseout24H     MissingAlarmCloseout           `json:"missing_alarm_closeout_24h"`
	StateConsistencyCloseout24H StateConsistencyCloseout       `json:"state_consistency_closeout_24h"`
	TargetBaseline              communityshorts.TargetBaseline `json:"target_baseline"`
	ChannelSummary              channelsummary.Report          `json:"channel_summary"`
	SendCounts                  sendcounts.Report              `json:"send_counts"`
	AlarmSentHistoryDataset     *alarmhistory.DatasetReport    `json:"alarm_sent_history_dataset,omitempty"`
	DeliveryLogs                deliverylogs.Report            `json:"delivery_logs"`
	LatencyPeriods              latencycause.PeriodReport      `json:"latency_periods"`
	LatencyCause                latencycause.Report            `json:"latency_cause"`
}

func DefaultPeriodSpecs() []latencycause.PeriodSpec {
	return []latencycause.PeriodSpec{
		{Label: "last_15m", Window: 15 * time.Minute},
		{Label: "last_1h", Window: time.Hour},
		{Label: "last_24h", Window: 24 * time.Hour},
	}
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
		return Report{}, fmt.Errorf("collect community shorts continuous observation report: config is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	now = shared.NormalizeSendCountTime(now)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	options = normalizeCollectOptions(options)
	if err := validateCollectOptions(options); err != nil {
		return Report{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}

	session, cleanupDB, err := shared.OpenOpsSession(ctx, appConfig, logger)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	return collectWithSession(ctx, session, appConfig, logger, now, options, collectorWiring{
		collectObservation:    collectObservationWindow,
		collectTargetBaseline: collectTargetBaselineWithSession,
		buildChannelSummary:   buildChannelSummary,
	})
}

func collectWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (Report, error) {
	artifacts, err := collectArtifacts(
		ctx,
		session,
		appConfig,
		logger,
		now,
		options,
		wiring,
	)
	if err != nil {
		return Report{}, fmt.Errorf("collect community shorts continuous observation report: %w", err)
	}

	return buildReport(now, artifacts), nil
}

func buildReport(
	now time.Time,
	artifacts artifacts,
) Report {
	return Report{
		GeneratedAt:                 now,
		Observation:                 artifacts.Observation,
		Closeout24H:                 buildCloseout24H(artifacts.Observation, artifacts.TargetBaseline, artifacts.SendCounts, artifacts.LatencyCause),
		MissingAlarmCloseout24H:     buildMissingAlarmCloseout(artifacts.Observation, artifacts.TargetBaseline, artifacts.AlarmSentHistoryDataset, artifacts.AlarmSentHistoryDatasetErr),
		StateConsistencyCloseout24H: buildStateConsistencyCloseout(artifacts.Observation, artifacts.TargetBaseline, artifacts.AlarmSentHistoryDataset, artifacts.AlarmSentHistoryDatasetErr),
		TargetBaseline:              artifacts.TargetBaseline,
		ChannelSummary:              artifacts.ChannelSummary,
		SendCounts:                  artifacts.SendCounts,
		AlarmSentHistoryDataset:     artifacts.AlarmSentHistoryDataset,
		DeliveryLogs:                artifacts.DeliveryLogs,
		LatencyPeriods:              artifacts.LatencyPeriods,
		LatencyCause:                artifacts.LatencyCause,
	}
}

func normalizeCollectOptions(
	options CollectOptions,
) CollectOptions {
	options.ObservationRuntimeName = strings.TrimSpace(options.ObservationRuntimeName)
	options.ObservationBigBangCutoverAt = shared.NormalizeSendCountTime(options.ObservationBigBangCutoverAt)
	if options.DeliveryLogLimit <= 0 {
		options.DeliveryLogLimit = defaultLogLimit
	}
	if len(options.LatencyPeriodSpecs) == 0 {
		options.LatencyPeriodSpecs = DefaultPeriodSpecs()
	}
	return options
}

func validateCollectOptions(
	options CollectOptions,
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

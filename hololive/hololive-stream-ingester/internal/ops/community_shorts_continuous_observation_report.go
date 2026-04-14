package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

type CommunityShortsContinuousObservationStatus string

const (
	CommunityShortsContinuousObservationStatusActive         CommunityShortsContinuousObservationStatus = "active"
	CommunityShortsContinuousObservationStatusFinalized      CommunityShortsContinuousObservationStatus = "finalized"
	communityShortsContinuousObservationDefaultLogLimit                                                 = 200
	communityShortsContinuousObservationCloseoutScope                                                   = "all_operational_channels"
	communityShortsContinuousObservationCloseoutRule                                                    = "observation status = finalized AND observation_window.internal_system_cause_posts == 0 (external_collection rows are excluded from pass/fail evaluation)"
	communityShortsContinuousObservationMissingAlarmRule                                                = "observation status = finalized AND sent_history_dataset.missing_alarm_posts == 0"
	communityShortsContinuousObservationStateConsistencyRule                                            = "observation status = finalized AND sent_history_dataset.duplicate_sent_posts == 0 AND sent_history_dataset.missing_alarm_posts == 0"
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

	observation, err := collectCommunityShortsContinuousObservationWindow(ctx, cfg, logger, now, options)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, err
	}

	targetBaseline, err := communityshorts.CollectTargetBaseline(ctx, cfg, logger)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: target baseline: %w", err)
	}

	sendCountReport, err := CollectCommunityShortsSendCountReportWithOptions(ctx, cfg, logger, now, CommunityShortsSendCountCollectOptions{
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: &options.ObservationBigBangCutoverAt,
	})
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: send counts: %w", err)
	}

	channelSummary, err := buildCommunityShortsContinuousObservationChannelSummary(sendCountReport)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: channel summary: %w", err)
	}

	deliveryLogReport, err := CollectCommunityShortsDeliveryLogReport(ctx, cfg, logger, now, CommunityShortsDeliveryLogCollectOptions{
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: &options.ObservationBigBangCutoverAt,
		Limit:                       options.DeliveryLogLimit,
	})
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: delivery logs: %w", err)
	}

	latencyCauseReport, err := CollectCommunityShortsLatencyCauseReportWithOptions(ctx, cfg, logger, now, CommunityShortsLatencyCauseCollectOptions{
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: &options.ObservationBigBangCutoverAt,
	})
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: latency cause: %w", err)
	}

	latencyPeriodReport, err := CollectCommunityShortsLatencyPeriodReport(ctx, cfg, logger, now, options.LatencyPeriodSpecs)
	if err != nil {
		return CommunityShortsContinuousObservationReport{}, fmt.Errorf("collect community shorts continuous observation report: latency periods: %w", err)
	}

	var alarmSentHistoryDataset *CommunityShortsAlarmSentHistoryDatasetReport
	var alarmSentHistoryDatasetErr error
	if observation.Status == CommunityShortsContinuousObservationStatusFinalized {
		dataset, datasetErr := CollectCommunityShortsAlarmSentHistoryDatasetReport(ctx, cfg, logger, now, CommunityShortsAlarmSentHistoryDatasetCollectOptions{
			ObservationRuntimeName:      options.ObservationRuntimeName,
			ObservationBigBangCutoverAt: &options.ObservationBigBangCutoverAt,
		})
		if datasetErr != nil {
			alarmSentHistoryDatasetErr = datasetErr
		} else {
			alarmSentHistoryDataset = &dataset
		}
	}

	closeout := buildCommunityShortsContinuousObservation24HCloseout(
		observation,
		targetBaseline,
		sendCountReport,
		latencyCauseReport,
	)
	missingAlarmCloseout := buildCommunityShortsContinuousObservationMissingAlarmCloseout(
		observation,
		targetBaseline,
		alarmSentHistoryDataset,
		alarmSentHistoryDatasetErr,
	)
	stateConsistencyCloseout := buildCommunityShortsContinuousObservationStateConsistencyCloseout(
		observation,
		targetBaseline,
		alarmSentHistoryDataset,
		alarmSentHistoryDatasetErr,
	)

	return CommunityShortsContinuousObservationReport{
		GeneratedAt:                 now,
		Observation:                 observation,
		Closeout24H:                 closeout,
		MissingAlarmCloseout24H:     missingAlarmCloseout,
		StateConsistencyCloseout24H: stateConsistencyCloseout,
		TargetBaseline:              targetBaseline,
		ChannelSummary:              channelSummary,
		SendCounts:                  sendCountReport,
		AlarmSentHistoryDataset:     alarmSentHistoryDataset,
		DeliveryLogs:                deliveryLogReport,
		LatencyPeriods:              latencyPeriodReport,
		LatencyCause:                latencyCauseReport,
	}, nil
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

func collectCommunityShortsContinuousObservationWindow(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
) (CommunityShortsContinuousObservationWindow, error) {
	databaseResources, cleanupDB, err := sharedproviders.ProvideDatabaseResources(ctx, cfg.Postgres, logger)
	if err != nil {
		return CommunityShortsContinuousObservationWindow{}, fmt.Errorf("collect community shorts continuous observation report: provide database resources: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		trackingrepo.NewRepository(databaseResources.Service.GetGormDB()),
		options.ObservationRuntimeName,
		options.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return CommunityShortsContinuousObservationWindow{}, fmt.Errorf("collect community shorts continuous observation report: find observation window: %w", err)
	}
	if state.Window == nil {
		return CommunityShortsContinuousObservationWindow{}, fmt.Errorf(
			"collect community shorts continuous observation report: observation window not found: runtime=%s cutover=%s",
			options.ObservationRuntimeName,
			formatCommunityShortsSendCountTime(options.ObservationBigBangCutoverAt),
		)
	}

	status := CommunityShortsContinuousObservationStatusActive
	if state.Finalized {
		status = CommunityShortsContinuousObservationStatusFinalized
	}

	return CommunityShortsContinuousObservationWindow{
		RuntimeName:           state.Window.RuntimeName,
		BigBangCutoverAt:      normalizeCommunityShortsSendCountTime(state.Window.BigBangCutoverAt),
		AppVersion:            strings.TrimSpace(state.Window.AppVersion),
		TargetChannelCount:    state.Window.TargetChannelCount,
		DeploymentCompletedAt: normalizeCommunityShortsSendCountTime(state.Window.DeploymentCompletedAt),
		ObservationStartedAt:  normalizeCommunityShortsSendCountTime(state.Window.ObservationStartedAt),
		ObservationEndsAt:     normalizeCommunityShortsSendCountTime(state.Window.ObservationEndedAt),
		ObservedUntil:         normalizeCommunityShortsSendCountTime(state.EffectiveWindowEnd),
		Status:                status,
	}, nil
}

func buildCommunityShortsContinuousObservationChannelSummary(
	report CommunityShortsSendCountReport,
) (CommunityShortsChannelSummaryReport, error) {
	posts := make([]outbox.PostSendCount, 0, len(report.Rows))
	for i := range report.Rows {
		posts = append(posts, report.Rows[i].PostSendCount)
	}

	rows, err := outbox.BuildChannelPostDeliverySummaries(posts)
	if err != nil {
		return CommunityShortsChannelSummaryReport{}, err
	}

	return BuildCommunityShortsChannelSummaryReport(rows, report.WindowEnd, report.WindowStart), nil
}

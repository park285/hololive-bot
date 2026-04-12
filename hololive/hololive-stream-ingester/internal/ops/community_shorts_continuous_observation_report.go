package ops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"
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
	TargetBaseline              runtimeapp.CommunityShortsTargetBaseline                     `json:"target_baseline"`
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

	targetBaseline, err := runtimeapp.CollectCommunityShortsTargetBaseline(ctx, cfg, logger)
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

func RenderCommunityShortsContinuousObservationMarkdown(report CommunityShortsContinuousObservationReport) string {
	var builder strings.Builder
	closeout := report.Closeout24H
	if closeout.Status == "" {
		closeout = buildCommunityShortsContinuousObservation24HCloseout(
			report.Observation,
			report.TargetBaseline,
			report.SendCounts,
			report.LatencyCause,
		)
	}
	missingAlarmCloseout := report.MissingAlarmCloseout24H
	if missingAlarmCloseout.Status == "" {
		missingAlarmCloseout = buildCommunityShortsContinuousObservationMissingAlarmCloseout(
			report.Observation,
			report.TargetBaseline,
			report.AlarmSentHistoryDataset,
			nil,
		)
	}

	stateConsistencyCloseout := report.StateConsistencyCloseout24H
	if stateConsistencyCloseout.Status == "" {
		stateConsistencyCloseout = buildCommunityShortsContinuousObservationStateConsistencyCloseout(
			report.Observation,
			report.TargetBaseline,
			report.AlarmSentHistoryDataset,
			nil,
		)
	}

	builder.WriteString("# YouTube Community/Shorts Continuous Observation Report\n\n")
	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation runtime: `")
	builder.WriteString(strings.TrimSpace(report.Observation.RuntimeName))
	builder.WriteString("`, cutover: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.BigBangCutoverAt))
	builder.WriteString("`\n")
	builder.WriteString("- observation status: `")
	builder.WriteString(string(report.Observation.Status))
	builder.WriteString("`\n")
	builder.WriteString("- observation window: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservationStartedAt))
	builder.WriteString("` -> `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservationEndsAt))
	builder.WriteString("`\n")
	builder.WriteString("- deployment completed at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.DeploymentCompletedAt))
	builder.WriteString("`, observed until: `")
	builder.WriteString(formatCommunityShortsSendCountTime(report.Observation.ObservedUntil))
	builder.WriteString("`\n")
	builder.WriteString("- target channels: `")
	builder.WriteString(fmt.Sprintf("%d", report.Observation.TargetChannelCount))
	builder.WriteString("`, app version: `")
	builder.WriteString(strings.TrimSpace(report.Observation.AppVersion))
	builder.WriteString("`\n")
	builder.WriteString("\n## 24h Closeout\n\n")
	builder.WriteString("- scope: `")
	builder.WriteString(fallbackCommunityShortsSendCountValue(closeout.AggregationScope))
	builder.WriteString("`, target_channels=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.TargetChannelCount))
	builder.WriteString("`, observed_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.ObservedPostCount))
	builder.WriteString("`, period_label=`")
	builder.WriteString(fallbackCommunityShortsSendCountValue(closeout.ObservationPeriodLabel))
	builder.WriteString("`\n")
	builder.WriteString("- internal over-2m closeout: status=`")
	builder.WriteString(string(closeout.Status))
	builder.WriteString("`, internal_system_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.InternalExceededPostCount))
	builder.WriteString("`, over_2m_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.TotalExceededPostCount))
	builder.WriteString("`, non_internal_system_cause_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.NonInternalExceededPostCount))
	builder.WriteString("`, excluded_external_collection_posts=`")
	builder.WriteString(fmt.Sprintf("%d", closeout.ExcludedExternalExceededPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(closeout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- closeout statement: ")
	builder.WriteString(closeout.Statement)
	builder.WriteString("\n")
	builder.WriteString("- missing alarm closeout: status=`")
	builder.WriteString(string(missingAlarmCloseout.Status))
	builder.WriteString("`, reference_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.ReferencePostCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.SendStatePostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", missingAlarmCloseout.NotSentMissingPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(missingAlarmCloseout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- missing alarm statement: ")
	builder.WriteString(missingAlarmCloseout.Statement)
	builder.WriteString("\n")
	builder.WriteString("- state consistency closeout: status=`")
	builder.WriteString(string(stateConsistencyCloseout.Status))
	builder.WriteString("`, reference_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.ReferencePostCount))
	builder.WriteString("`, send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.SendStatePostCount))
	builder.WriteString("`, duplicate_sent_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.DuplicateSentPostCount))
	builder.WriteString("`, missing_alarm_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.MissingAlarmPostCount))
	builder.WriteString("`, missing_send_state_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.MissingSendStatePostCount))
	builder.WriteString("`, attempted_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.AttemptedMissingPostCount))
	builder.WriteString("`, not_sent_missing_posts=`")
	builder.WriteString(fmt.Sprintf("%d", stateConsistencyCloseout.NotSentMissingPostCount))
	builder.WriteString("`, rule=`")
	builder.WriteString(stateConsistencyCloseout.Rule)
	builder.WriteString("`\n")
	builder.WriteString("- state consistency statement: ")
	builder.WriteString(stateConsistencyCloseout.Statement)
	builder.WriteString("\n")

	builder.WriteString("\n## Target Baseline\n\n")
	builder.WriteString(renderCommunityShortsContinuousObservationTargetBaseline(report.TargetBaseline))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsChannelSummaryMarkdown(report.ChannelSummary), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsSendCountMarkdown(report.SendCounts), 1))
	builder.WriteString("\n")
	if report.AlarmSentHistoryDataset != nil {
		builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(*report.AlarmSentHistoryDataset), 1))
		builder.WriteString("\n")
	}
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsDeliveryLogMarkdown(report.DeliveryLogs), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsLatencyPeriodMarkdown(report.LatencyPeriods), 1))
	builder.WriteString("\n")
	builder.WriteString(promoteCommunityShortsMarkdownHeadings(RenderCommunityShortsLatencyCauseMarkdown(report.LatencyCause), 1))
	builder.WriteString("\n")

	return builder.String()
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

func buildCommunityShortsContinuousObservation24HCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline runtimeapp.CommunityShortsTargetBaseline,
	sendCounts CommunityShortsSendCountReport,
	latencyCause CommunityShortsLatencyCauseReport,
) CommunityShortsContinuousObservation24HCloseout {
	closeout := CommunityShortsContinuousObservation24HCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		ObservedPostCount:  sendCounts.Summary.PostCount,
		Rule:               communityShortsContinuousObservationCloseoutRule,
		Statement:          "24h closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}

	period, ok := findCommunityShortsObservationLatencyCausePeriod(latencyCause)
	if !ok {
		if observation.Status == CommunityShortsContinuousObservationStatusFinalized {
			closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
			closeout.Statement = "Finalized 24h closeout is blocked because the observation_window latency cause summary is missing."
		}
		return closeout
	}

	closeout.ObservationPeriodLabel = strings.TrimSpace(period.Summary.Label)
	closeout.TotalExceededPostCount = period.CauseSummary.ExceededPostCount
	closeout.InternalExceededPostCount = period.CauseSummary.InternalSystemCausePostCount
	closeout.NonInternalExceededPostCount = period.CauseSummary.NonInternalSystemCausePostCount
	closeout.ExcludedExternalExceededPostCount = period.CauseSummary.ExcludedExternalDelayPostCount
	if closeout.ExcludedExternalExceededPostCount == 0 && period.CauseSummary.ExternalCollectionSourcePostCount > 0 {
		closeout.ExcludedExternalExceededPostCount = period.CauseSummary.ExternalCollectionSourcePostCount
	}

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h closeout is pending until observation status becomes finalized; current observation_window internal_system_cause_posts=%d while excluded external_collection posts=%d remain logged across all operational channels.",
			closeout.InternalExceededPostCount,
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	if closeout.InternalExceededPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=0; excluded external_collection posts=%d remain logged but do not affect pass/fail evaluation.",
			closeout.ExcludedExternalExceededPostCount,
		)
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded internal_system_cause_posts=%d; excluded external_collection posts=%d remain outside pass/fail evaluation.",
		closeout.InternalExceededPostCount,
		closeout.ExcludedExternalExceededPostCount,
	)
	return closeout
}

func buildCommunityShortsContinuousObservationMissingAlarmCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline runtimeapp.CommunityShortsTargetBaseline,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
	datasetErr error,
) CommunityShortsContinuousObservationMissingAlarmCloseout {
	closeout := CommunityShortsContinuousObservationMissingAlarmCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               communityShortsContinuousObservationMissingAlarmRule,
		Statement:          "24h missing-alarm closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	if dataset != nil {
		closeout.ReferencePostCount = dataset.Summary.ReferenceRowCount
		closeout.SendStatePostCount = dataset.Summary.SendStatePostCount
		closeout.MissingAlarmPostCount = dataset.Summary.MissingAlarmPostCount
		closeout.MissingSendStatePostCount = dataset.Summary.MissingSendStatePostCount
		closeout.AttemptedMissingPostCount = dataset.Summary.AttemptedMissingPostCount
		closeout.NotSentMissingPostCount = dataset.Summary.NotSentMissingPostCount
	}

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h missing-alarm closeout is pending until observation status becomes finalized; current missing_alarm_posts=%d across all operational channels.",
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
		if datasetErr != nil {
			closeout.Statement = fmt.Sprintf(
				"Finalized 24h missing-alarm closeout is blocked because the sent-history dataset could not be collected: %v.",
				datasetErr,
			)
		} else {
			closeout.Statement = "Finalized 24h missing-alarm closeout is blocked because the sent-history dataset is missing."
		}
		return closeout
	}

	if closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = fmt.Sprintf(
			"Finalized 24h observation across all operational channels recorded missing_alarm_posts=0 out of reference_posts=%d.",
			closeout.ReferencePostCount,
		)
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded missing_alarm_posts=%d out of reference_posts=%d.",
		closeout.MissingAlarmPostCount,
		closeout.ReferencePostCount,
	)
	return closeout
}

func buildCommunityShortsContinuousObservationStateConsistencyCloseout(
	observation CommunityShortsContinuousObservationWindow,
	baseline runtimeapp.CommunityShortsTargetBaseline,
	dataset *CommunityShortsAlarmSentHistoryDatasetReport,
	datasetErr error,
) CommunityShortsContinuousObservationStateConsistencyCloseout {
	closeout := CommunityShortsContinuousObservationStateConsistencyCloseout{
		Status:             CommunityShortsContinuousObservationCloseoutStatusPending,
		AggregationScope:   communityShortsContinuousObservationCloseoutScope,
		TargetChannelCount: observation.TargetChannelCount,
		Rule:               communityShortsContinuousObservationStateConsistencyRule,
		Statement:          "24h state-consistency closeout is pending until the observation window is finalized.",
	}
	if baseline.Runtime.TargetChannelCount > 0 {
		closeout.TargetChannelCount = baseline.Runtime.TargetChannelCount
	}
	if dataset != nil {
		closeout.ReferencePostCount = dataset.Summary.ReferenceRowCount
		closeout.SendStatePostCount = dataset.Summary.SendStatePostCount
		closeout.DuplicateSentPostCount = dataset.Summary.DuplicateSentPostCount
		closeout.MissingAlarmPostCount = dataset.Summary.MissingAlarmPostCount
		closeout.MissingSendStatePostCount = dataset.Summary.MissingSendStatePostCount
		closeout.AttemptedMissingPostCount = dataset.Summary.AttemptedMissingPostCount
		closeout.NotSentMissingPostCount = dataset.Summary.NotSentMissingPostCount
	}

	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		closeout.Statement = fmt.Sprintf(
			"24h state-consistency closeout is pending until observation status becomes finalized; current duplicate_sent_posts=%d and missing_alarm_posts=%d across all operational channels.",
			closeout.DuplicateSentPostCount,
			closeout.MissingAlarmPostCount,
		)
		return closeout
	}

	if dataset == nil {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence
		if datasetErr != nil {
			closeout.Statement = fmt.Sprintf(
				"Finalized 24h state-consistency closeout is blocked because the sent-history dataset could not be collected: %v.",
				datasetErr,
			)
		} else {
			closeout.Statement = "Finalized 24h state-consistency closeout is blocked because the sent-history dataset is missing."
		}
		return closeout
	}

	if closeout.DuplicateSentPostCount == 0 && closeout.MissingAlarmPostCount == 0 {
		closeout.Status = CommunityShortsContinuousObservationCloseoutStatusPass
		closeout.Statement = "Finalized 24h observation across all operational channels recorded duplicate_sent_posts=0 and missing_alarm_posts=0; every reference post converged to a single completed sent state."
		return closeout
	}

	closeout.Status = CommunityShortsContinuousObservationCloseoutStatusFail
	closeout.Statement = fmt.Sprintf(
		"Finalized 24h observation across all operational channels recorded duplicate_sent_posts=%d and missing_alarm_posts=%d; recovery did not converge all reference posts to a single completed sent state.",
		closeout.DuplicateSentPostCount,
		closeout.MissingAlarmPostCount,
	)
	return closeout
}

func findCommunityShortsObservationLatencyCausePeriod(
	report CommunityShortsLatencyCauseReport,
) (CommunityShortsLatencyCausePeriodView, bool) {
	for i := range report.Periods {
		if strings.TrimSpace(report.Periods[i].Summary.Label) == communityShortsLatencyCauseObservationPeriodLabel {
			return report.Periods[i], true
		}
	}
	return CommunityShortsLatencyCausePeriodView{}, false
}

func renderCommunityShortsContinuousObservationTargetBaseline(
	baseline runtimeapp.CommunityShortsTargetBaseline,
) string {
	var builder strings.Builder

	builder.WriteString("- generated at: `")
	builder.WriteString(formatCommunityShortsSendCountTime(baseline.GeneratedAt))
	builder.WriteString("`\n")
	builder.WriteString("- final delivery owner: `")
	builder.WriteString(strings.TrimSpace(baseline.Runtime.FinalDeliveryOwner))
	builder.WriteString("`, big-bang enabled: `")
	builder.WriteString(formatCommunityShortsContinuousObservationBool(baseline.Runtime.CommunityShortsBigBangEnabled))
	builder.WriteString("`\n")
	builder.WriteString("- runtime target channels: `")
	builder.WriteString(fmt.Sprintf("%d", baseline.Runtime.TargetChannelCount))
	builder.WriteString("`, channel rows: `")
	builder.WriteString(fmt.Sprintf("%d", len(baseline.Channels)))
	builder.WriteString("`\n")

	if len(baseline.Channels) == 0 {
		builder.WriteString("\n활성 운영 채널 baseline이 없습니다.\n")
		return builder.String()
	}

	builder.WriteString("\n| channel_id | owner | community_enabled | community_rooms | community_mode | shorts_enabled | shorts_rooms | shorts_mode |\n")
	builder.WriteString("| --- | --- | --- | ---: | --- | --- | ---: | --- |\n")
	for i := range baseline.Channels {
		communityRoute, _ := runtimeapp.CommunityShortsRouteForType(baseline.Channels[i].Routes, domain.AlarmTypeCommunity)
		shortsRoute, _ := runtimeapp.CommunityShortsRouteForType(baseline.Channels[i].Routes, domain.AlarmTypeShorts)
		builder.WriteString("| `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(baseline.Channels[i].ChannelID))
		builder.WriteString("` | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(baseline.Channels[i].OwnerLabel))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsContinuousObservationBool(communityRoute.AlarmEnabled))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", communityRoute.SubscriberRoomCount))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(communityRoute.EffectiveDeliveryMode))
		builder.WriteString("` | `")
		builder.WriteString(formatCommunityShortsContinuousObservationBool(shortsRoute.AlarmEnabled))
		builder.WriteString("` | ")
		builder.WriteString(fmt.Sprintf("%d", shortsRoute.SubscriberRoomCount))
		builder.WriteString(" | `")
		builder.WriteString(fallbackCommunityShortsSendCountValue(shortsRoute.EffectiveDeliveryMode))
		builder.WriteString("` |\n")
	}

	return builder.String()
}

func promoteCommunityShortsMarkdownHeadings(markdown string, depth int) string {
	if depth <= 0 || strings.TrimSpace(markdown) == "" {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	prefix := strings.Repeat("#", depth)
	for i := range lines {
		trimmed := strings.TrimLeft(lines[i], " ")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		heading := trimmed
		count := 0
		for count < len(heading) && heading[count] == '#' {
			count++
		}
		if count == 0 || count >= len(heading) || heading[count] != ' ' {
			continue
		}
		indent := lines[i][:len(lines[i])-len(trimmed)]
		lines[i] = indent + prefix + heading
	}
	return strings.Join(lines, "\n")
}

func formatCommunityShortsContinuousObservationBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

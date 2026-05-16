package reports

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedproviders "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"

	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

type communityShortsContinuousObservationArtifacts struct {
	Observation                CommunityShortsContinuousObservationWindow
	TargetBaseline             communityshorts.TargetBaseline
	ChannelSummary             CommunityShortsChannelSummaryReport
	SendCounts                 CommunityShortsSendCountReport
	AlarmSentHistoryDataset    *CommunityShortsAlarmSentHistoryDatasetReport
	AlarmSentHistoryDatasetErr error
	DeliveryLogs               CommunityShortsDeliveryLogReport
	LatencyPeriods             CommunityShortsLatencyPeriodReport
	LatencyCause               CommunityShortsLatencyCauseReport
}

type communityShortsContinuousObservationCollectorWiring struct {
	collectObservation             func(context.Context, *communityShortsOpsSession, time.Time, CommunityShortsContinuousObservationCollectOptions) (CommunityShortsContinuousObservationWindow, error)
	collectTargetBaseline          func(context.Context, *communityShortsOpsSession, *config.Config, *slog.Logger, time.Time) (communityshorts.TargetBaseline, error)
	collectSendCounts              func(context.Context, *communityShortsOpsSession, CommunityShortsSendCountQuery, time.Time) (CommunityShortsSendCountReport, error)
	buildChannelSummary            func(CommunityShortsSendCountReport) (CommunityShortsChannelSummaryReport, error)
	collectDeliveryLogs            func(context.Context, *communityShortsOpsSession, CommunityShortsDeliveryLogQuery, time.Time) (CommunityShortsDeliveryLogReport, error)
	collectLatencyCause            func(context.Context, *communityShortsOpsSession, CommunityShortsLatencyCauseQuery, time.Time, []outbox.PostLatencyPeriod) (CommunityShortsLatencyCauseReport, error)
	buildLatencyPeriods            func(time.Time, []CommunityShortsLatencyPeriodSpec) ([]outbox.PostLatencyPeriod, error)
	collectLatencyPeriods          func(context.Context, *communityShortsOpsSession, time.Time, []outbox.PostLatencyPeriod) (CommunityShortsLatencyPeriodReport, error)
	collectAlarmSentHistoryDataset func(context.Context, *communityShortsOpsSession, time.Time, CommunityShortsAlarmSentHistoryDatasetQuery) (CommunityShortsAlarmSentHistoryDatasetReport, error)
}

func defaultCommunityShortsContinuousObservationCollectorWiring() communityShortsContinuousObservationCollectorWiring {
	return communityShortsContinuousObservationCollectorWiring{
		collectObservation:             collectCommunityShortsContinuousObservationWindow,
		collectTargetBaseline:          collectCommunityShortsTargetBaselineWithSession,
		collectSendCounts:              collectCommunityShortsSendCountReportWithSession,
		buildChannelSummary:            buildCommunityShortsContinuousObservationChannelSummary,
		collectDeliveryLogs:            collectCommunityShortsDeliveryLogReportWithSession,
		collectLatencyCause:            collectCommunityShortsLatencyCauseReportWithSession,
		buildLatencyPeriods:            buildCommunityShortsLatencyPeriods,
		collectLatencyPeriods:          collectCommunityShortsLatencyPeriodReportWithSession,
		collectAlarmSentHistoryDataset: collectCommunityShortsAlarmSentHistoryDatasetReportWithSession,
	}
}

func collectCommunityShortsContinuousObservationArtifacts(
	ctx context.Context,
	session *communityShortsOpsSession,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (communityShortsContinuousObservationArtifacts, error) {
	observation, err := wiring.collectObservation(ctx, session, now, options)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, err
	}

	targetBaseline, err := wiring.collectTargetBaseline(ctx, session, cfg, logger, now)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("target baseline: %w", err)
	}

	sendCountReport, err := collectCommunityShortsContinuousObservationSendCounts(ctx, session, now, options, wiring)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("send counts: %w", err)
	}

	channelSummary, err := wiring.buildChannelSummary(sendCountReport)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("channel summary: %w", err)
	}

	deliveryLogs, err := collectCommunityShortsContinuousObservationDeliveryLogs(ctx, session, now, options, wiring)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("delivery logs: %w", err)
	}

	latencyCause, err := collectCommunityShortsContinuousObservationLatencyCause(ctx, session, now, options, wiring)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("latency cause: %w", err)
	}

	latencyPeriodReport, err := collectCommunityShortsContinuousObservationLatencyPeriods(ctx, session, now, options, wiring)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("latency periods: %w", err)
	}

	alarmSentHistoryDataset, alarmSentHistoryDatasetErr := collectCommunityShortsContinuousObservationAlarmSentHistoryDataset(
		ctx, session, now, options, observation, wiring,
	)

	return communityShortsContinuousObservationArtifacts{
		Observation:                observation,
		TargetBaseline:             targetBaseline,
		ChannelSummary:             channelSummary,
		SendCounts:                 sendCountReport,
		AlarmSentHistoryDataset:    alarmSentHistoryDataset,
		AlarmSentHistoryDatasetErr: alarmSentHistoryDatasetErr,
		DeliveryLogs:               deliveryLogs,
		LatencyPeriods:             latencyPeriodReport,
		LatencyCause:               latencyCause,
	}, nil
}

func collectCommunityShortsContinuousObservationSendCounts(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (CommunityShortsSendCountReport, error) {
	query := CommunityShortsSendCountQuery{
		Mode:                        communityShortsSendCountQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	return wiring.collectSendCounts(ctx, session, query, now)
}

func collectCommunityShortsContinuousObservationDeliveryLogs(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (CommunityShortsDeliveryLogReport, error) {
	query := CommunityShortsDeliveryLogQuery{
		Mode:                        communityShortsDeliveryLogQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
		Limit:                       options.DeliveryLogLimit,
	}
	return wiring.collectDeliveryLogs(ctx, session, query, now)
}

func collectCommunityShortsContinuousObservationLatencyCause(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (CommunityShortsLatencyCauseReport, error) {
	query := CommunityShortsLatencyCauseQuery{
		Mode:                        communityShortsLatencyCauseQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	return wiring.collectLatencyCause(ctx, session, query, now, nil)
}

func collectCommunityShortsContinuousObservationLatencyPeriods(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	wiring communityShortsContinuousObservationCollectorWiring,
) (CommunityShortsLatencyPeriodReport, error) {
	latencyPeriods, err := wiring.buildLatencyPeriods(now, options.LatencyPeriodSpecs)
	if err != nil {
		return CommunityShortsLatencyPeriodReport{}, err
	}
	return wiring.collectLatencyPeriods(ctx, session, now, latencyPeriods)
}

func collectCommunityShortsContinuousObservationAlarmSentHistoryDataset(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
	observation CommunityShortsContinuousObservationWindow,
	wiring communityShortsContinuousObservationCollectorWiring,
) (*CommunityShortsAlarmSentHistoryDatasetReport, error) {
	if observation.Status != CommunityShortsContinuousObservationStatusFinalized {
		return nil, nil
	}

	query := CommunityShortsAlarmSentHistoryDatasetQuery{
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	dataset, err := wiring.collectAlarmSentHistoryDataset(ctx, session, now, query)
	if err != nil {
		return nil, err
	}
	return &dataset, nil
}

func collectCommunityShortsTargetBaselineWithSession(
	ctx context.Context,
	session *communityShortsOpsSession,
	cfg *config.Config,
	logger *slog.Logger,
	now time.Time,
) (communityshorts.TargetBaseline, error) {
	if session == nil || session.postgres == nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("session is nil")
	}

	memberRepository := sharedproviders.ProvideMemberRepository(session.postgres, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(session.postgres, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("load alarms: %w", err)
	}

	channels := communityshorts.BuildOperationalChannelsFromMembers(members)
	baseline, err := communityshorts.BuildTargetBaseline(channels, alarms, cfg.Ingestion, now)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("build target baseline: %w", err)
	}
	return baseline, nil
}

func collectCommunityShortsContinuousObservationWindow(
	ctx context.Context,
	session *communityShortsOpsSession,
	now time.Time,
	options CommunityShortsContinuousObservationCollectOptions,
) (CommunityShortsContinuousObservationWindow, error) {
	if session == nil {
		return CommunityShortsContinuousObservationWindow{}, fmt.Errorf("collect community shorts continuous observation report: session is nil")
	}

	state, err := resolveCommunityShortsObservationQueryState(
		ctx,
		session.trackingRepository,
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
		return CommunityShortsChannelSummaryReport{}, fmt.Errorf("build channel delivery summaries: %w", err)
	}

	return BuildCommunityShortsChannelSummaryReport(rows, report.WindowEnd, report.WindowStart), nil
}

package ops

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

	sendCountQuery := CommunityShortsSendCountQuery{
		Mode:                        communityShortsSendCountQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	sendCountReport, err := wiring.collectSendCounts(ctx, session, sendCountQuery, now)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("send counts: %w", err)
	}

	channelSummary, err := wiring.buildChannelSummary(sendCountReport)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("channel summary: %w", err)
	}

	deliveryLogQuery := CommunityShortsDeliveryLogQuery{
		Mode:                        communityShortsDeliveryLogQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
		Limit:                       options.DeliveryLogLimit,
	}
	deliveryLogs, err := wiring.collectDeliveryLogs(ctx, session, deliveryLogQuery, now)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("delivery logs: %w", err)
	}

	latencyCauseQuery := CommunityShortsLatencyCauseQuery{
		Mode:                        communityShortsLatencyCauseQueryModeObservation,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	latencyCause, err := wiring.collectLatencyCause(ctx, session, latencyCauseQuery, now, nil)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("latency cause: %w", err)
	}

	latencyPeriods, err := wiring.buildLatencyPeriods(now, options.LatencyPeriodSpecs)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("latency periods: %w", err)
	}
	latencyPeriodReport, err := wiring.collectLatencyPeriods(ctx, session, now, latencyPeriods)
	if err != nil {
		return communityShortsContinuousObservationArtifacts{}, fmt.Errorf("latency periods: %w", err)
	}

	var alarmSentHistoryDataset *CommunityShortsAlarmSentHistoryDatasetReport
	var alarmSentHistoryDatasetErr error
	if observation.Status == CommunityShortsContinuousObservationStatusFinalized {
		datasetQuery := CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      options.ObservationRuntimeName,
			ObservationBigBangCutoverAt: cloneCommunityShortsSendCountTime(&options.ObservationBigBangCutoverAt),
		}
		dataset, datasetErr := wiring.collectAlarmSentHistoryDataset(ctx, session, now, datasetQuery)
		if datasetErr != nil {
			alarmSentHistoryDatasetErr = datasetErr
		} else {
			alarmSentHistoryDataset = &dataset
		}
	}

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
	return communityshorts.BuildTargetBaseline(channels, alarms, cfg.Ingestion, now)
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
		return CommunityShortsChannelSummaryReport{}, err
	}

	return BuildCommunityShortsChannelSummaryReport(rows, report.WindowEnd, report.WindowStart), nil
}

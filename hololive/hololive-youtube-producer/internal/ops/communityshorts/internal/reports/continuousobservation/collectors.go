package continuousobservation

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

	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/channelsummary"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/deliverylogs"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

const (
	observationQueryMode sendcounts.QueryMode = "observation_window"
)

type artifacts struct {
	Observation                Window
	TargetBaseline             communityshorts.TargetBaseline
	ChannelSummary             channelsummary.Report
	SendCounts                 sendcounts.Report
	AlarmSentHistoryDataset    *alarmhistory.DatasetReport
	AlarmSentHistoryDatasetErr error
	DeliveryLogs               deliverylogs.Report
	LatencyPeriods             latencycause.PeriodReport
	LatencyCause               latencycause.Report
}

type collectorWiring struct {
	collectObservation             func(context.Context, *shared.OpsSession, time.Time, CollectOptions) (Window, error)
	collectTargetBaseline          func(context.Context, *shared.OpsSession, *config.Config, *slog.Logger, time.Time) (communityshorts.TargetBaseline, error)
	collectSendCounts              func(context.Context, *shared.OpsSession, sendcounts.Query, time.Time) (sendcounts.Report, error)
	buildChannelSummary            func(sendcounts.Report) (channelsummary.Report, error)
	collectDeliveryLogs            func(context.Context, *shared.OpsSession, deliverylogs.Query, time.Time) (deliverylogs.Report, error)
	collectLatencyCause            func(context.Context, *shared.OpsSession, latencycause.Query, time.Time, []outbox.PostLatencyPeriod) (latencycause.Report, error)
	buildLatencyPeriods            func(time.Time, []latencycause.PeriodSpec) ([]outbox.PostLatencyPeriod, error)
	collectLatencyPeriods          func(context.Context, *shared.OpsSession, time.Time, []outbox.PostLatencyPeriod) (latencycause.PeriodReport, error)
	collectAlarmSentHistoryDataset func(context.Context, *shared.OpsSession, time.Time, alarmhistory.DatasetQuery) (alarmhistory.DatasetReport, error)
}

func collectArtifacts(
	ctx context.Context,
	session *shared.OpsSession,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (artifacts, error) {
	observation, err := wiring.collectObservation(ctx, session, now, options)
	if err != nil {
		return artifacts{}, err
	}

	targetBaseline, err := wiring.collectTargetBaseline(ctx, session, appConfig, logger, now)
	if err != nil {
		return artifacts{}, fmt.Errorf("target baseline: %w", err)
	}

	sendCountReport, err := collectSendCounts(ctx, session, now, options, wiring)
	if err != nil {
		return artifacts{}, fmt.Errorf("send counts: %w", err)
	}

	channelSummary, err := wiring.buildChannelSummary(sendCountReport)
	if err != nil {
		return artifacts{}, fmt.Errorf("channel summary: %w", err)
	}

	deliveryLogs, err := collectDeliveryLogs(ctx, session, now, options, wiring)
	if err != nil {
		return artifacts{}, fmt.Errorf("delivery logs: %w", err)
	}

	latencyCause, err := collectLatencyCause(ctx, session, now, options, wiring)
	if err != nil {
		return artifacts{}, fmt.Errorf("latency cause: %w", err)
	}

	latencyPeriodReport, err := collectLatencyPeriods(ctx, session, now, options, wiring)
	if err != nil {
		return artifacts{}, fmt.Errorf("latency periods: %w", err)
	}

	alarmSentHistoryDataset, alarmSentHistoryDatasetErr := collectAlarmSentHistoryDataset(
		ctx, session, now, options, observation, wiring,
	)

	return artifacts{
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

func collectSendCounts(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (sendcounts.Report, error) {
	query := sendcounts.Query{
		Mode:                        observationQueryMode,
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	return wiring.collectSendCounts(ctx, session, query, now)
}

func collectDeliveryLogs(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (deliverylogs.Report, error) {
	query := deliverylogs.Query{
		Mode:                        deliverylogs.QueryMode(observationQueryMode),
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(&options.ObservationBigBangCutoverAt),
		Limit:                       options.DeliveryLogLimit,
	}
	return wiring.collectDeliveryLogs(ctx, session, query, now)
}

func collectLatencyCause(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (latencycause.Report, error) {
	query := latencycause.Query{
		Mode:                        latencycause.QueryMode(observationQueryMode),
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	return wiring.collectLatencyCause(ctx, session, query, now, nil)
}

func collectLatencyPeriods(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
	wiring collectorWiring,
) (latencycause.PeriodReport, error) {
	specs := make([]latencycause.PeriodSpec, 0, len(options.LatencyPeriodSpecs))
	for _, s := range options.LatencyPeriodSpecs {
		specs = append(specs, latencycause.PeriodSpec{Label: s.Label, Window: s.Window})
	}
	latencyPeriods, err := wiring.buildLatencyPeriods(now, specs)
	if err != nil {
		return latencycause.PeriodReport{}, err
	}
	return wiring.collectLatencyPeriods(ctx, session, now, latencyPeriods)
}

func collectAlarmSentHistoryDataset(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
	observation Window,
	wiring collectorWiring,
) (*alarmhistory.DatasetReport, error) {
	if observation.Status != StatusFinalized {
		return nil, nil
	}

	query := alarmhistory.DatasetQuery{
		ObservationRuntimeName:      options.ObservationRuntimeName,
		ObservationBigBangCutoverAt: shared.CloneSendCountTime(&options.ObservationBigBangCutoverAt),
	}
	dataset, err := wiring.collectAlarmSentHistoryDataset(ctx, session, now, query)
	if err != nil {
		return nil, err
	}
	return &dataset, nil
}

func collectTargetBaselineWithSession(
	ctx context.Context,
	session *shared.OpsSession,
	appConfig *config.Config,
	logger *slog.Logger,
	now time.Time,
) (communityshorts.TargetBaseline, error) {
	if session == nil || session.Postgres == nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("session is nil")
	}

	memberRepository := sharedproviders.ProvideMemberRepository(session.Postgres, logger)
	members, err := memberRepository.GetAllMembers(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("load members: %w", err)
	}

	alarmRepository := sharedalarm.NewRepository(session.Postgres, logger)
	alarms, err := alarmRepository.LoadAll(ctx)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("load alarms: %w", err)
	}

	channels := communityshorts.BuildOperationalChannelsFromMembers(members)
	baseline, err := communityshorts.BuildTargetBaseline(channels, alarms, appConfig.Ingestion, now)
	if err != nil {
		return communityshorts.TargetBaseline{}, fmt.Errorf("build target baseline: %w", err)
	}
	return baseline, nil
}

func collectObservationWindow(
	ctx context.Context,
	session *shared.OpsSession,
	now time.Time,
	options CollectOptions,
) (Window, error) {
	if session == nil {
		return Window{}, fmt.Errorf("collect community shorts continuous observation report: session is nil")
	}

	state, err := shared.ResolveObservationQueryState(
		ctx,
		session.TrackingRepository,
		options.ObservationRuntimeName,
		options.ObservationBigBangCutoverAt,
		now,
	)
	if err != nil {
		return Window{}, fmt.Errorf("collect community shorts continuous observation report: find observation window: %w", err)
	}
	if state.Window == nil {
		return Window{}, fmt.Errorf(
			"collect community shorts continuous observation report: observation window not found: runtime=%s cutover=%s",
			options.ObservationRuntimeName,
			shared.FormatSendCountTime(options.ObservationBigBangCutoverAt),
		)
	}

	status := StatusActive
	if state.Finalized {
		status = StatusFinalized
	}

	return Window{
		RuntimeName:           state.Window.RuntimeName,
		BigBangCutoverAt:      shared.NormalizeSendCountTime(state.Window.BigBangCutoverAt),
		AppVersion:            strings.TrimSpace(state.Window.AppVersion),
		TargetChannelCount:    state.Window.TargetChannelCount,
		DeploymentCompletedAt: shared.NormalizeSendCountTime(state.Window.DeploymentCompletedAt),
		ObservationStartedAt:  shared.NormalizeSendCountTime(state.Window.ObservationStartedAt),
		ObservationEndsAt:     shared.NormalizeSendCountTime(state.Window.ObservationEndedAt),
		ObservedUntil:         shared.NormalizeSendCountTime(state.EffectiveWindowEnd),
		Status:                status,
	}, nil
}

func buildChannelSummary(
	report sendcounts.Report,
) (channelsummary.Report, error) {
	posts := make([]outbox.PostSendCount, 0, len(report.Rows))
	for i := range report.Rows {
		posts = append(posts, report.Rows[i].PostSendCount)
	}

	rows, err := outbox.BuildChannelPostDeliverySummaries(posts)
	if err != nil {
		return channelsummary.Report{}, fmt.Errorf("build channel delivery summaries: %w", err)
	}

	return channelsummary.Build(rows, report.WindowEnd, report.WindowStart), nil
}

package continuousobservation

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/alarmhistory"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/channelsummary"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/deliverylogs"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/latencycause"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/sendcounts"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/internal/reports/shared"
)

func TestRenderMarkdownIncludes24HCloseout(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	observationStart := cutoverAt.Add(2 * time.Minute)
	observationEnd := observationStart.Add(24 * time.Hour)
	generatedAt := observationEnd

	report := Report{
		GeneratedAt: generatedAt,
		Observation: Window{
			RuntimeName:           "youtube-producer",
			BigBangCutoverAt:      cutoverAt,
			AppVersion:            "2.0.99",
			TargetChannelCount:    2,
			DeploymentCompletedAt: observationStart,
			ObservationStartedAt:  observationStart,
			ObservationEndsAt:     observationEnd,
			ObservedUntil:         observationEnd,
			Status:                StatusFinalized,
		},
		TargetBaseline: communityshorts.TargetBaseline{
			GeneratedAt: generatedAt,
			Runtime: communityshorts.TargetBaselineRuntime{
				FinalDeliveryOwner:            "alarm-worker",
				CommunityShortsBigBangEnabled: true,
				TargetChannelCount:            2,
			},
			Channels: []communityshorts.TargetBaselineChannel{{
				OwnerLabel: "Member A",
				ChannelID:  "UC_A",
				Routes: []communityshorts.TargetBaselineChannelRoute{
					{AlarmType: domain.AlarmTypeCommunity, AlarmEnabled: true, SubscriberRoomCount: 3, EffectiveDeliveryMode: "new_only"},
					{AlarmType: domain.AlarmTypeShorts, AlarmEnabled: true, SubscriberRoomCount: 2, EffectiveDeliveryMode: "new_only"},
				},
			}},
		},
		ChannelSummary: channelsummary.Report{
			GeneratedAt: generatedAt,
			WindowStart: observationStart,
			WindowEnd:   observationEnd,
		},
		SendCounts: sendcounts.Report{
			GeneratedAt: generatedAt,
			Query: sendcounts.Query{
				Mode:                        "observation_window",
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
			WindowStart: observationStart,
			WindowEnd:   observationEnd,
			Summary: sendcounts.Summary{
				PostCount: 2,
			},
		},
		AlarmSentHistoryDataset: &alarmhistory.DatasetReport{
			GeneratedAt: generatedAt,
			Query: alarmhistory.DatasetQuery{
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
			},
			Summary: alarmhistory.DatasetSummary{
				ReferenceRowCount:  2,
				SendStatePostCount: 2,
			},
		},
		DeliveryLogs: deliverylogs.Report{
			GeneratedAt: generatedAt,
			Query: deliverylogs.Query{
				Mode: "observation_window",
			},
		},
		LatencyPeriods: latencycause.PeriodReport{GeneratedAt: generatedAt},
		LatencyCause: latencycause.Report{
			GeneratedAt: generatedAt,
			Query: latencycause.Query{
				Mode:                        "observation_window",
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
			Periods: []latencycause.PeriodView{{
				Summary: outbox.PostLatencyPeriodSummary{
					Label:             observationPeriodLabel,
					StartAt:           observationStart,
					EndAt:             observationEnd,
					ExceededPostCount: 1,
				},
				CauseSummary: latencycause.Summary{
					ExceededPostCount:                 1,
					InternalSystemCausePostCount:      0,
					NonInternalSystemCausePostCount:   1,
					ExcludedExternalDelayPostCount:    1,
					ExternalCollectionSourcePostCount: 1,
				},
			}},
		},
	}

	markdown := RenderMarkdown(&report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Continuous Observation Report")
	require.Contains(t, markdown, "## 24h Closeout")
	require.Contains(t, markdown, "scope: `all_operational_channels`, target_channels=`2`, observed_posts=`2`, period_label=`observation_window`")
	require.Contains(t, markdown, "internal over-2m closeout: status=`pass`, internal_system_cause_posts=`0`, over_2m_posts=`1`, non_internal_system_cause_posts=`1`, excluded_external_collection_posts=`1`")
	require.Contains(t, markdown, "Finalized 24h observation across all operational channels recorded internal_system_cause_posts=0; excluded external_collection posts=1 remain logged but do not affect pass/fail evaluation.")
	require.Contains(t, markdown, "missing alarm closeout: status=`pass`, reference_posts=`2`, send_state_posts=`2`, missing_alarm_posts=`0`")
	require.Contains(t, markdown, "Finalized 24h observation across all operational channels recorded missing_alarm_posts=0 out of reference_posts=2.")
	require.Contains(t, markdown, "state consistency closeout: status=`pass`, reference_posts=`2`, send_state_posts=`2`, duplicate_sent_posts=`0`, missing_alarm_posts=`0`")
	require.Contains(t, markdown, "Finalized 24h observation across all operational channels recorded duplicate_sent_posts=0 and missing_alarm_posts=0; every reference post converged to a single completed sent state.")
	require.Contains(t, markdown, "## Target Baseline")
	require.Contains(t, markdown, "## YouTube Community/Shorts Channel Delivery Summary")
	require.Contains(t, markdown, "## YouTube Community/Shorts Alarm Sent History Dataset")
}

func TestRenderMarkdownPromotesEmbeddedDatasetSections(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	observationStart := cutoverAt.Add(2 * time.Minute)
	observationEnd := observationStart.Add(24 * time.Hour)
	generatedAt := observationEnd

	markdown := RenderMarkdown(&Report{
		GeneratedAt: generatedAt,
		Observation: Window{
			RuntimeName:           "youtube-producer",
			BigBangCutoverAt:      cutoverAt,
			AppVersion:            "2.0.99",
			TargetChannelCount:    1,
			DeploymentCompletedAt: observationStart,
			ObservationStartedAt:  observationStart,
			ObservationEndsAt:     observationEnd,
			ObservedUntil:         observationEnd,
			Status:                StatusFinalized,
		},
		TargetBaseline: communityshorts.TargetBaseline{
			GeneratedAt: generatedAt,
			Runtime: communityshorts.TargetBaselineRuntime{
				FinalDeliveryOwner:            "alarm-worker",
				CommunityShortsBigBangEnabled: true,
				TargetChannelCount:            1,
			},
		},
		ChannelSummary: channelsummary.Report{GeneratedAt: generatedAt},
		SendCounts: sendcounts.Report{
			GeneratedAt: generatedAt,
			Query: sendcounts.Query{
				Mode:                        "observation_window",
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
		},
		AlarmSentHistoryDataset: &alarmhistory.DatasetReport{
			GeneratedAt: generatedAt,
			Query: alarmhistory.DatasetQuery{
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
			},
		},
		DeliveryLogs:   deliverylogs.Report{GeneratedAt: generatedAt},
		LatencyPeriods: latencycause.PeriodReport{GeneratedAt: generatedAt},
		LatencyCause: latencycause.Report{
			GeneratedAt: generatedAt,
			Query: latencycause.Query{
				Mode:                        "observation_window",
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-producer",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
		},
	})

	require.Contains(t, markdown, "## YouTube Community/Shorts Alarm Sent History Dataset")
	require.Contains(t, markdown, "### Results")
	require.Contains(t, markdown, "### Missing Alarm Rows")
	require.Contains(t, markdown, "### Verification Rows")
	require.Contains(t, markdown, "### Normalized Verification Reference Rows")
	require.Contains(t, markdown, "### Normalized Sent History Rows")
}

func TestBuildCloseout24HPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	observationStart := time.Date(2026, 4, 11, 0, 2, 0, 0, time.UTC)
	observationEnd := observationStart.Add(24 * time.Hour)

	observation := Window{
		TargetChannelCount:   2,
		ObservationStartedAt: observationStart,
		ObservationEndsAt:    observationEnd,
		ObservedUntil:        observationStart.Add(30 * time.Minute),
		Status:               StatusActive,
	}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	sendCounts := sendcounts.Report{Summary: sendcounts.Summary{PostCount: 5}}
	latencyCause := latencycause.Report{Periods: []latencycause.PeriodView{{
		Summary: outbox.PostLatencyPeriodSummary{Label: observationPeriodLabel},
		CauseSummary: latencycause.Summary{
			ExceededPostCount:                 3,
			InternalSystemCausePostCount:      2,
			NonInternalSystemCausePostCount:   1,
			ExcludedExternalDelayPostCount:    1,
			ExternalCollectionSourcePostCount: 1,
		},
	}}}
	closeout := buildCloseout24H(&observation, &baseline, &sendCounts, &latencyCause)

	require.Equal(t, CloseoutStatusPending, closeout.Status)
	require.Equal(t, "all_operational_channels", closeout.AggregationScope)
	require.Equal(t, 2, closeout.TargetChannelCount)
	require.Equal(t, 5, closeout.ObservedPostCount)
	require.Equal(t, int64(2), closeout.InternalExceededPostCount)
	require.Equal(t, int64(3), closeout.TotalExceededPostCount)
	require.Equal(t, int64(1), closeout.ExcludedExternalExceededPostCount)
	require.Equal(t, "observation_window", closeout.ObservationPeriodLabel)
	require.Contains(t, closeout.Statement, "pending until observation status becomes finalized")
	require.Contains(t, closeout.Statement, "internal_system_cause_posts=2")
	require.Contains(t, closeout.Statement, "excluded external_collection posts=1")
}

func TestBuildCloseout24HFinalizedPassExcludesExternalCollectionPosts(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	sendCounts := sendcounts.Report{Summary: sendcounts.Summary{PostCount: 4}}
	latencyCause := latencycause.Report{Periods: []latencycause.PeriodView{{
		Summary: outbox.PostLatencyPeriodSummary{Label: observationPeriodLabel},
		CauseSummary: latencycause.Summary{
			ExceededPostCount:                 2,
			InternalSystemCausePostCount:      0,
			NonInternalSystemCausePostCount:   2,
			ExcludedExternalDelayPostCount:    2,
			ExternalCollectionSourcePostCount: 2,
		},
	}}}
	closeout := buildCloseout24H(&observation, &baseline, &sendCounts, &latencyCause)

	require.Equal(t, CloseoutStatusPass, closeout.Status)
	require.Equal(t, int64(2), closeout.TotalExceededPostCount)
	require.Equal(t, int64(0), closeout.InternalExceededPostCount)
	require.Equal(t, int64(2), closeout.NonInternalExceededPostCount)
	require.Equal(t, int64(2), closeout.ExcludedExternalExceededPostCount)
	require.Contains(t, closeout.Rule, "external_collection rows are excluded")
	require.Contains(t, closeout.Statement, "excluded external_collection posts=2")
}

func TestBuildCloseout24HUsesInsufficientEvidenceWhenFinalizedSummaryMissing(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	sendCounts := sendcounts.Report{Summary: sendcounts.Summary{PostCount: 1}}
	latencyCause := latencycause.Report{}
	closeout := buildCloseout24H(&observation, &baseline, &sendCounts, &latencyCause)

	require.Equal(t, CloseoutStatusInsufficientEvidence, closeout.Status)
	require.Equal(t, int64(0), closeout.InternalExceededPostCount)
	require.Contains(t, closeout.Statement, "latency cause summary is missing")
}

func TestBuildMissingAlarmCloseoutPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusActive}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildMissingAlarmCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{MissingAlarmPostCount: 1}},
		nil,
	)

	require.Equal(t, CloseoutStatusPending, closeout.Status)
	require.Equal(t, "all_operational_channels", closeout.AggregationScope)
	require.Equal(t, 2, closeout.TargetChannelCount)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Statement, "pending until observation status becomes finalized")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildMissingAlarmCloseoutFinalizedPass(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildMissingAlarmCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{
			ReferenceRowCount:  4,
			SendStatePostCount: 4,
		}},
		nil,
	)

	require.Equal(t, CloseoutStatusPass, closeout.Status)
	require.Equal(t, 4, closeout.ReferencePostCount)
	require.Equal(t, 4, closeout.SendStatePostCount)
	require.Equal(t, 0, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Rule, "missing_alarm_posts == 0")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=0")
}

func TestBuildMissingAlarmCloseoutFinalizedFail(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildMissingAlarmCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{
			ReferenceRowCount:         5,
			SendStatePostCount:        4,
			MissingAlarmPostCount:     1,
			MissingSendStatePostCount: 1,
			AttemptedMissingPostCount: 1,
		}},
		nil,
	)

	require.Equal(t, CloseoutStatusFail, closeout.Status)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Equal(t, 1, closeout.MissingSendStatePostCount)
	require.Equal(t, 1, closeout.AttemptedMissingPostCount)
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildMissingAlarmCloseoutUsesInsufficientEvidenceWhenDatasetMissing(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildMissingAlarmCloseout(
		&observation,
		&baseline,
		nil,
		fmt.Errorf("dataset unavailable"),
	)

	require.Equal(t, CloseoutStatusInsufficientEvidence, closeout.Status)
	require.Contains(t, closeout.Statement, "sent-history dataset could not be collected")
	require.Contains(t, closeout.Statement, "dataset unavailable")
}

func TestBuildStateConsistencyCloseoutPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusActive}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildStateConsistencyCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{
			ReferenceRowCount:         3,
			SendStatePostCount:        2,
			DuplicateSentPostCount:    1,
			MissingAlarmPostCount:     1,
			AttemptedMissingPostCount: 1,
		}},
		nil,
	)

	require.Equal(t, CloseoutStatusPending, closeout.Status)
	require.Equal(t, "all_operational_channels", closeout.AggregationScope)
	require.Equal(t, 2, closeout.TargetChannelCount)
	require.Equal(t, 3, closeout.ReferencePostCount)
	require.Equal(t, 2, closeout.SendStatePostCount)
	require.Equal(t, 1, closeout.DuplicateSentPostCount)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Statement, "pending until observation status becomes finalized")
	require.Contains(t, closeout.Statement, "duplicate_sent_posts=1")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildStateConsistencyCloseoutFinalizedPass(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildStateConsistencyCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{
			ReferenceRowCount:      4,
			SendStatePostCount:     4,
			DuplicateSentPostCount: 0,
			MissingAlarmPostCount:  0,
		}},
		nil,
	)

	require.Equal(t, CloseoutStatusPass, closeout.Status)
	require.Equal(t, 4, closeout.ReferencePostCount)
	require.Equal(t, 4, closeout.SendStatePostCount)
	require.Equal(t, 0, closeout.DuplicateSentPostCount)
	require.Equal(t, 0, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Rule, "duplicate_sent_posts == 0")
	require.Contains(t, closeout.Rule, "missing_alarm_posts == 0")
	require.Contains(t, closeout.Statement, "duplicate_sent_posts=0 and missing_alarm_posts=0")
}

func TestBuildStateConsistencyCloseoutFinalizedFail(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildStateConsistencyCloseout(
		&observation,
		&baseline,
		&alarmhistory.DatasetReport{Summary: alarmhistory.DatasetSummary{
			ReferenceRowCount:         5,
			SendStatePostCount:        4,
			DuplicateSentPostCount:    1,
			MissingAlarmPostCount:     1,
			MissingSendStatePostCount: 1,
			AttemptedMissingPostCount: 1,
			NotSentMissingPostCount:   1,
		}},
		nil,
	)

	require.Equal(t, CloseoutStatusFail, closeout.Status)
	require.Equal(t, 1, closeout.DuplicateSentPostCount)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Equal(t, 1, closeout.MissingSendStatePostCount)
	require.Equal(t, 1, closeout.AttemptedMissingPostCount)
	require.Equal(t, 1, closeout.NotSentMissingPostCount)
	require.Contains(t, closeout.Statement, "duplicate_sent_posts=1")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildStateConsistencyCloseoutUsesInsufficientEvidenceWhenDatasetMissing(t *testing.T) {
	t.Parallel()

	observation := Window{TargetChannelCount: 2, Status: StatusFinalized}
	baseline := communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}}
	closeout := buildStateConsistencyCloseout(
		&observation,
		&baseline,
		nil,
		fmt.Errorf("dataset unavailable"),
	)

	require.Equal(t, CloseoutStatusInsufficientEvidence, closeout.Status)
	require.Contains(t, closeout.Statement, "sent-history dataset could not be collected")
	require.Contains(t, closeout.Statement, "dataset unavailable")
}

func TestCollectArtifactsCollectsFinalizedDataset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cutoverAt := now.Add(-24 * time.Hour)
	periods := []outbox.PostLatencyPeriod{{Label: "last_24h", StartAt: now.Add(-24 * time.Hour), EndAt: now}}
	options := CollectOptions{
		ObservationRuntimeName:      "youtube-producer",
		ObservationBigBangCutoverAt: cutoverAt,
		DeliveryLogLimit:            25,
		LatencyPeriodSpecs:          DefaultPeriodSpecs()[:1],
	}
	expectedObservation := Window{
		RuntimeName:           options.ObservationRuntimeName,
		BigBangCutoverAt:      cutoverAt,
		ObservationStartedAt:  cutoverAt,
		ObservationEndsAt:     now,
		ObservedUntil:         now,
		Status:                StatusFinalized,
		TargetChannelCount:    2,
		DeploymentCompletedAt: cutoverAt,
	}
	expectedBaseline := communityshorts.TargetBaseline{
		Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2},
	}
	expectedSendCounts := sendcounts.Report{
		Summary: sendcounts.Summary{PostCount: 3},
	}
	expectedChannelSummary := channelsummary.Report{
		GeneratedAt: now,
	}
	expectedDeliveryLogs := deliverylogs.Report{
		Summary: deliverylogs.Summary{LogCount: 2},
	}
	expectedLatencyCause := latencycause.Report{
		Periods: []latencycause.PeriodView{{
			Summary: outbox.PostLatencyPeriodSummary{Label: observationPeriodLabel},
		}},
	}
	expectedLatencyPeriods := latencycause.PeriodReport{
		Periods: []outbox.PostLatencyPeriodSummary{{Label: "last_24h"}},
	}
	expectedDataset := alarmhistory.DatasetReport{
		Summary: alarmhistory.DatasetSummary{ReferenceRowCount: 3},
	}

	callLog := make([]string, 0, 9)
	result, err := collectArtifacts(
		context.Background(),
		nil,
		nil,
		nil,
		now,
		options,
		collectorWiring{
			collectObservation: func(ctx context.Context, session *shared.OpsSession, now time.Time, opts CollectOptions) (Window, error) {
				callLog = append(callLog, "observation")
				require.Equal(t, opts.ObservationRuntimeName, expectedObservation.RuntimeName)
				return expectedObservation, nil
			},
			collectTargetBaseline: func(ctx context.Context, session *shared.OpsSession, appConfig *config.Config, logger *slog.Logger, now time.Time) (communityshorts.TargetBaseline, error) {
				callLog = append(callLog, "baseline")
				return expectedBaseline, nil
			},
			collectSendCounts: func(ctx context.Context, session *shared.OpsSession, query sendcounts.Query, now time.Time) (sendcounts.Report, error) {
				callLog = append(callLog, "send-counts")
				require.Equal(t, options.ObservationRuntimeName, query.ObservationRuntimeName)
				require.Equal(t, cutoverAt, *query.ObservationBigBangCutoverAt)
				return expectedSendCounts, nil
			},
			buildChannelSummary: func(report *sendcounts.Report) (channelsummary.Report, error) {
				callLog = append(callLog, "channel-summary")
				require.NotNil(t, report)
				require.Equal(t, expectedSendCounts, *report)
				return expectedChannelSummary, nil
			},
			collectDeliveryLogs: func(ctx context.Context, session *shared.OpsSession, query deliverylogs.Query, now time.Time) (deliverylogs.Report, error) {
				callLog = append(callLog, "delivery-logs")
				require.Equal(t, options.DeliveryLogLimit, query.Limit)
				return expectedDeliveryLogs, nil
			},
			collectLatencyCause: func(ctx context.Context, session *shared.OpsSession, query latencycause.Query, now time.Time, p []outbox.PostLatencyPeriod) (latencycause.Report, error) {
				callLog = append(callLog, "latency-cause")
				require.Nil(t, p)
				return expectedLatencyCause, nil
			},
			buildLatencyPeriods: func(now time.Time, specs []latencycause.PeriodSpec) ([]outbox.PostLatencyPeriod, error) {
				callLog = append(callLog, "build-periods")
				require.Len(t, specs, 1)
				return periods, nil
			},
			collectLatencyPeriods: func(ctx context.Context, session *shared.OpsSession, now time.Time, got []outbox.PostLatencyPeriod) (latencycause.PeriodReport, error) {
				callLog = append(callLog, "latency-periods")
				require.Equal(t, periods, got)
				return expectedLatencyPeriods, nil
			},
			collectAlarmSentHistoryDataset: func(ctx context.Context, session *shared.OpsSession, now time.Time, query alarmhistory.DatasetQuery) (alarmhistory.DatasetReport, error) {
				callLog = append(callLog, "dataset")
				require.Equal(t, options.ObservationRuntimeName, query.ObservationRuntimeName)
				require.Equal(t, cutoverAt, *query.ObservationBigBangCutoverAt)
				return expectedDataset, nil
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []string{
		"observation",
		"baseline",
		"send-counts",
		"channel-summary",
		"delivery-logs",
		"latency-cause",
		"build-periods",
		"latency-periods",
		"dataset",
	}, callLog)
	require.Equal(t, expectedObservation, result.Observation)
	require.Equal(t, expectedBaseline, result.TargetBaseline)
	require.Equal(t, expectedSendCounts, result.SendCounts)
	require.Equal(t, expectedChannelSummary, result.ChannelSummary)
	require.Equal(t, expectedDeliveryLogs, result.DeliveryLogs)
	require.Equal(t, expectedLatencyCause, result.LatencyCause)
	require.Equal(t, expectedLatencyPeriods, result.LatencyPeriods)
	require.NotNil(t, result.AlarmSentHistoryDataset)
	require.Equal(t, expectedDataset, *result.AlarmSentHistoryDataset)
	require.NoError(t, result.AlarmSentHistoryDatasetErr)
}

func TestCollectArtifactsSkipsDatasetUntilFinalized(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cutoverAt := now.Add(-2 * time.Hour)
	options := CollectOptions{
		ObservationRuntimeName:      "youtube-producer",
		ObservationBigBangCutoverAt: cutoverAt,
		DeliveryLogLimit:            10,
		LatencyPeriodSpecs:          DefaultPeriodSpecs()[:1],
	}

	datasetCalled := false
	result, err := collectArtifacts(
		context.Background(),
		nil,
		nil,
		nil,
		now,
		options,
		collectorWiring{
			collectObservation: func(ctx context.Context, session *shared.OpsSession, now time.Time, opts CollectOptions) (Window, error) {
				return Window{
					RuntimeName:           opts.ObservationRuntimeName,
					BigBangCutoverAt:      opts.ObservationBigBangCutoverAt,
					ObservationStartedAt:  cutoverAt,
					ObservationEndsAt:     now,
					ObservedUntil:         now,
					Status:                StatusActive,
					TargetChannelCount:    1,
					DeploymentCompletedAt: cutoverAt,
				}, nil
			},
			collectTargetBaseline: func(ctx context.Context, session *shared.OpsSession, appConfig *config.Config, logger *slog.Logger, now time.Time) (communityshorts.TargetBaseline, error) {
				return communityshorts.TargetBaseline{}, nil
			},
			collectSendCounts: func(ctx context.Context, session *shared.OpsSession, query sendcounts.Query, now time.Time) (sendcounts.Report, error) {
				return sendcounts.Report{}, nil
			},
			buildChannelSummary: func(report *sendcounts.Report) (channelsummary.Report, error) {
				return channelsummary.Report{}, nil
			},
			collectDeliveryLogs: func(ctx context.Context, session *shared.OpsSession, query deliverylogs.Query, now time.Time) (deliverylogs.Report, error) {
				return deliverylogs.Report{}, nil
			},
			collectLatencyCause: func(ctx context.Context, session *shared.OpsSession, query latencycause.Query, now time.Time, p []outbox.PostLatencyPeriod) (latencycause.Report, error) {
				return latencycause.Report{}, nil
			},
			buildLatencyPeriods: func(now time.Time, specs []latencycause.PeriodSpec) ([]outbox.PostLatencyPeriod, error) {
				return []outbox.PostLatencyPeriod{}, nil
			},
			collectLatencyPeriods: func(ctx context.Context, session *shared.OpsSession, now time.Time, periods []outbox.PostLatencyPeriod) (latencycause.PeriodReport, error) {
				return latencycause.PeriodReport{}, nil
			},
			collectAlarmSentHistoryDataset: func(ctx context.Context, session *shared.OpsSession, now time.Time, query alarmhistory.DatasetQuery) (alarmhistory.DatasetReport, error) {
				datasetCalled = true
				return alarmhistory.DatasetReport{}, nil
			},
		},
	)
	require.NoError(t, err)
	require.False(t, datasetCalled)
	require.Nil(t, result.AlarmSentHistoryDataset)
	require.NoError(t, result.AlarmSentHistoryDatasetErr)
}

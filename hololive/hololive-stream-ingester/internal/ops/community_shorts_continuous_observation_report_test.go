package ops

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

func TestRenderCommunityShortsContinuousObservationMarkdownIncludes24HCloseout(t *testing.T) {
	t.Parallel()

	cutoverAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	observationStart := cutoverAt.Add(2 * time.Minute)
	observationEnd := observationStart.Add(24 * time.Hour)
	generatedAt := observationEnd

	report := CommunityShortsContinuousObservationReport{
		GeneratedAt: generatedAt,
		Observation: CommunityShortsContinuousObservationWindow{
			RuntimeName:           "youtube-scraper",
			BigBangCutoverAt:      cutoverAt,
			AppVersion:            "2.0.99",
			TargetChannelCount:    2,
			DeploymentCompletedAt: observationStart,
			ObservationStartedAt:  observationStart,
			ObservationEndsAt:     observationEnd,
			ObservedUntil:         observationEnd,
			Status:                CommunityShortsContinuousObservationStatusFinalized,
		},
		TargetBaseline: communityshorts.TargetBaseline{
			GeneratedAt: generatedAt,
			Runtime: communityshorts.TargetBaselineRuntime{
				FinalDeliveryOwner:            "youtube-scraper",
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
		ChannelSummary: CommunityShortsChannelSummaryReport{
			GeneratedAt: generatedAt,
			WindowStart: observationStart,
			WindowEnd:   observationEnd,
		},
		SendCounts: CommunityShortsSendCountReport{
			GeneratedAt: generatedAt,
			Query: CommunityShortsSendCountQuery{
				Mode:                        communityShortsSendCountQueryModeObservation,
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-scraper",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
			WindowStart: observationStart,
			WindowEnd:   observationEnd,
			Summary: CommunityShortsSendCountSummary{
				PostCount: 2,
			},
		},
		AlarmSentHistoryDataset: &CommunityShortsAlarmSentHistoryDatasetReport{
			GeneratedAt: generatedAt,
			Query: CommunityShortsAlarmSentHistoryDatasetQuery{
				ObservationRuntimeName:      "youtube-scraper",
				ObservationBigBangCutoverAt: &cutoverAt,
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
			},
			Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
				ReferenceRowCount:  2,
				SendStatePostCount: 2,
			},
		},
		DeliveryLogs: CommunityShortsDeliveryLogReport{
			GeneratedAt: generatedAt,
			Query: CommunityShortsDeliveryLogQuery{
				Mode: communityShortsDeliveryLogQueryModeObservation,
			},
		},
		LatencyPeriods: CommunityShortsLatencyPeriodReport{GeneratedAt: generatedAt},
		LatencyCause: CommunityShortsLatencyCauseReport{
			GeneratedAt: generatedAt,
			Query: CommunityShortsLatencyCauseQuery{
				Mode:                        communityShortsLatencyCauseQueryModeObservation,
				WindowStart:                 &observationStart,
				WindowEnd:                   &observationEnd,
				ObservationRuntimeName:      "youtube-scraper",
				ObservationBigBangCutoverAt: &cutoverAt,
			},
			Periods: []CommunityShortsLatencyCausePeriodView{{
				Summary: outbox.PostLatencyPeriodSummary{
					Label:             communityShortsLatencyCauseObservationPeriodLabel,
					StartAt:           observationStart,
					EndAt:             observationEnd,
					ExceededPostCount: 1,
				},
				CauseSummary: CommunityShortsLatencyCauseSummary{
					ExceededPostCount:                 1,
					InternalSystemCausePostCount:      0,
					NonInternalSystemCausePostCount:   1,
					ExcludedExternalDelayPostCount:    1,
					ExternalCollectionSourcePostCount: 1,
				},
			}},
		},
	}

	markdown := RenderCommunityShortsContinuousObservationMarkdown(report)
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

func TestBuildCommunityShortsContinuousObservation24HCloseoutPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	observationStart := time.Date(2026, 4, 11, 0, 2, 0, 0, time.UTC)
	observationEnd := observationStart.Add(24 * time.Hour)

	closeout := buildCommunityShortsContinuousObservation24HCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount:   2,
			ObservationStartedAt: observationStart,
			ObservationEndsAt:    observationEnd,
			ObservedUntil:        observationStart.Add(30 * time.Minute),
			Status:               CommunityShortsContinuousObservationStatusActive,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		CommunityShortsSendCountReport{Summary: CommunityShortsSendCountSummary{PostCount: 5}},
		CommunityShortsLatencyCauseReport{Periods: []CommunityShortsLatencyCausePeriodView{{
			Summary: outbox.PostLatencyPeriodSummary{Label: communityShortsLatencyCauseObservationPeriodLabel},
			CauseSummary: CommunityShortsLatencyCauseSummary{
				ExceededPostCount:                 3,
				InternalSystemCausePostCount:      2,
				NonInternalSystemCausePostCount:   1,
				ExcludedExternalDelayPostCount:    1,
				ExternalCollectionSourcePostCount: 1,
			},
		}}},
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPending, closeout.Status)
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

func TestBuildCommunityShortsContinuousObservation24HCloseoutFinalizedPassExcludesExternalCollectionPosts(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservation24HCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		CommunityShortsSendCountReport{Summary: CommunityShortsSendCountSummary{PostCount: 4}},
		CommunityShortsLatencyCauseReport{Periods: []CommunityShortsLatencyCausePeriodView{{
			Summary: outbox.PostLatencyPeriodSummary{Label: communityShortsLatencyCauseObservationPeriodLabel},
			CauseSummary: CommunityShortsLatencyCauseSummary{
				ExceededPostCount:                 2,
				InternalSystemCausePostCount:      0,
				NonInternalSystemCausePostCount:   2,
				ExcludedExternalDelayPostCount:    2,
				ExternalCollectionSourcePostCount: 2,
			},
		}}},
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPass, closeout.Status)
	require.Equal(t, int64(2), closeout.TotalExceededPostCount)
	require.Equal(t, int64(0), closeout.InternalExceededPostCount)
	require.Equal(t, int64(2), closeout.NonInternalExceededPostCount)
	require.Equal(t, int64(2), closeout.ExcludedExternalExceededPostCount)
	require.Contains(t, closeout.Rule, "external_collection rows are excluded")
	require.Contains(t, closeout.Statement, "excluded external_collection posts=2")
}

func TestBuildCommunityShortsContinuousObservation24HCloseoutUsesInsufficientEvidenceWhenFinalizedSummaryMissing(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservation24HCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		CommunityShortsSendCountReport{Summary: CommunityShortsSendCountSummary{PostCount: 1}},
		CommunityShortsLatencyCauseReport{},
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence, closeout.Status)
	require.Equal(t, int64(0), closeout.InternalExceededPostCount)
	require.Contains(t, closeout.Statement, "latency cause summary is missing")
}

func TestBuildCommunityShortsContinuousObservationMissingAlarmCloseoutPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationMissingAlarmCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusActive,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{MissingAlarmPostCount: 1}},
		nil,
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPending, closeout.Status)
	require.Equal(t, "all_operational_channels", closeout.AggregationScope)
	require.Equal(t, 2, closeout.TargetChannelCount)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Statement, "pending until observation status becomes finalized")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildCommunityShortsContinuousObservationMissingAlarmCloseoutFinalizedPass(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationMissingAlarmCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
			ReferenceRowCount:  4,
			SendStatePostCount: 4,
		}},
		nil,
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPass, closeout.Status)
	require.Equal(t, 4, closeout.ReferencePostCount)
	require.Equal(t, 4, closeout.SendStatePostCount)
	require.Equal(t, 0, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Rule, "missing_alarm_posts == 0")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=0")
}

func TestBuildCommunityShortsContinuousObservationMissingAlarmCloseoutFinalizedFail(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationMissingAlarmCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
			ReferenceRowCount:         5,
			SendStatePostCount:        4,
			MissingAlarmPostCount:     1,
			MissingSendStatePostCount: 1,
			AttemptedMissingPostCount: 1,
		}},
		nil,
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusFail, closeout.Status)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Equal(t, 1, closeout.MissingSendStatePostCount)
	require.Equal(t, 1, closeout.AttemptedMissingPostCount)
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildCommunityShortsContinuousObservationMissingAlarmCloseoutUsesInsufficientEvidenceWhenDatasetMissing(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationMissingAlarmCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		nil,
		fmt.Errorf("dataset unavailable"),
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence, closeout.Status)
	require.Contains(t, closeout.Statement, "sent-history dataset could not be collected")
	require.Contains(t, closeout.Statement, "dataset unavailable")
}

func TestBuildCommunityShortsContinuousObservationStateConsistencyCloseoutPendingUntilFinalized(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationStateConsistencyCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusActive,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
			ReferenceRowCount:         3,
			SendStatePostCount:        2,
			DuplicateSentPostCount:    1,
			MissingAlarmPostCount:     1,
			AttemptedMissingPostCount: 1,
		}},
		nil,
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPending, closeout.Status)
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

func TestBuildCommunityShortsContinuousObservationStateConsistencyCloseoutFinalizedPass(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationStateConsistencyCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
			ReferenceRowCount:      4,
			SendStatePostCount:     4,
			DuplicateSentPostCount: 0,
			MissingAlarmPostCount:  0,
		}},
		nil,
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPass, closeout.Status)
	require.Equal(t, 4, closeout.ReferencePostCount)
	require.Equal(t, 4, closeout.SendStatePostCount)
	require.Equal(t, 0, closeout.DuplicateSentPostCount)
	require.Equal(t, 0, closeout.MissingAlarmPostCount)
	require.Contains(t, closeout.Rule, "duplicate_sent_posts == 0")
	require.Contains(t, closeout.Rule, "missing_alarm_posts == 0")
	require.Contains(t, closeout.Statement, "duplicate_sent_posts=0 and missing_alarm_posts=0")
}

func TestBuildCommunityShortsContinuousObservationStateConsistencyCloseoutFinalizedFail(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationStateConsistencyCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		&CommunityShortsAlarmSentHistoryDatasetReport{Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
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

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusFail, closeout.Status)
	require.Equal(t, 1, closeout.DuplicateSentPostCount)
	require.Equal(t, 1, closeout.MissingAlarmPostCount)
	require.Equal(t, 1, closeout.MissingSendStatePostCount)
	require.Equal(t, 1, closeout.AttemptedMissingPostCount)
	require.Equal(t, 1, closeout.NotSentMissingPostCount)
	require.Contains(t, closeout.Statement, "duplicate_sent_posts=1")
	require.Contains(t, closeout.Statement, "missing_alarm_posts=1")
}

func TestBuildCommunityShortsContinuousObservationStateConsistencyCloseoutUsesInsufficientEvidenceWhenDatasetMissing(t *testing.T) {
	t.Parallel()

	closeout := buildCommunityShortsContinuousObservationStateConsistencyCloseout(
		CommunityShortsContinuousObservationWindow{
			TargetChannelCount: 2,
			Status:             CommunityShortsContinuousObservationStatusFinalized,
		},
		communityshorts.TargetBaseline{Runtime: communityshorts.TargetBaselineRuntime{TargetChannelCount: 2}},
		nil,
		fmt.Errorf("dataset unavailable"),
	)

	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence, closeout.Status)
	require.Contains(t, closeout.Statement, "sent-history dataset could not be collected")
	require.Contains(t, closeout.Statement, "dataset unavailable")
}

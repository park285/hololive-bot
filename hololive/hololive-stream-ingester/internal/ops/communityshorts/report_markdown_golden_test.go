package communityshortsops

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

func TestRenderCommunityShortsAlarmSentHistoryDatasetMarkdown_MatchesGolden(t *testing.T) {
	t.Parallel()

	assertMarkdownGolden(t,
		"community_shorts_alarm_sent_history_dataset.golden.md",
		RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(buildDatasetGoldenFixture()),
	)
}

func TestRenderCommunityShortsContinuousObservationMarkdown_MatchesGolden(t *testing.T) {
	t.Parallel()

	assertMarkdownGolden(t,
		"community_shorts_continuous_observation.golden.md",
		RenderCommunityShortsContinuousObservationMarkdown(buildContinuousObservationGoldenFixture()),
	)
}

func assertMarkdownGolden(t *testing.T, name string, markdown string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("golden 디렉터리 생성 실패: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(markdown), 0o644); err != nil {
			t.Fatalf("golden 파일 갱신 실패: %v", err)
		}
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden 파일 읽기 실패: %v", err)
	}

	if string(golden) != markdown {
		t.Fatalf("markdown golden mismatch for %s", name)
	}
}

func buildDatasetGoldenFixture() CommunityShortsAlarmSentHistoryDatasetReport {
	generatedAt := time.Date(2026, 4, 15, 12, 34, 56, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	windowStart := cutoverAt
	windowEnd := cutoverAt.Add(24 * time.Hour)
	publishedAt := cutoverAt.Add(2 * time.Hour)
	detectedAt := publishedAt.Add(30 * time.Second)
	alarmSentAt := publishedAt.Add(90 * time.Second)

	return CommunityShortsAlarmSentHistoryDatasetReport{
		GeneratedAt: generatedAt,
		Query: CommunityShortsAlarmSentHistoryDatasetQuery{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &windowStart,
			WindowEnd:                   &windowEnd,
		},
		Summary: CommunityShortsAlarmSentHistoryDatasetSummary{
			CollectedRowCount:         1,
			SentPostCount:             1,
			CommunitySentPostCount:    1,
			BaselinePostCount:         1,
			MatchedPostCount:          1,
			VerificationRowCount:      1,
			ReferenceRowCount:         1,
			SendStatePostCount:        1,
			EarliestAlarmSentAt:       &alarmSentAt,
			LatestAlarmSentAt:         &alarmSentAt,
			MissingAlarmPostCount:     0,
			MissingSendStatePostCount: 0,
		},
		Results: CommunityShortsAlarmSentHistoryDatasetResults{
			MissingAlarmEvaluated: true,
			MissingAlarmZero:      true,
			AlarmTypeComparisons: []CommunityShortsAlarmSentHistoryDatasetAlarmTypeComparison{{
				AlarmType:         domain.AlarmTypeCommunity,
				BaselinePostCount: 1,
				SentPostCount:     1,
				MatchedPostCount:  1,
			}},
			ChannelComparisons: []CommunityShortsAlarmSentHistoryDatasetChannelComparison{{
				ChannelID:         "UC_TEST",
				BaselinePostCount: 1,
				SentPostCount:     1,
				MatchedPostCount:  1,
			}},
		},
		Rows: []CommunityShortsAlarmSentHistoryDatasetRow{{
			AlarmType:         domain.AlarmTypeCommunity,
			PostKey:           "COMMUNITY|UC_TEST|community:test-post",
			PostID:            "community:test-post",
			ContentID:         "test-post",
			ChannelID:         "UC_TEST",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
			AlarmSentAt:       alarmSentAt,
		}},
		VerificationRows: []CommunityShortsAlarmSentHistoryDatasetVerificationRow{{
			Verdict:           trackingrepo.ObservationPostComparisonVerdictMatched,
			Reason:            trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_TEST",
			PostID:            "community:test-post",
			PostKey:           "COMMUNITY|UC_TEST|community:test-post",
			ContentID:         "test-post",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        &detectedAt,
			AlarmSentAt:       &alarmSentAt,
			BaselineCount:     1,
			SentCount:         1,
		}},
		ReferenceRows: []CommunityShortsAlarmSentHistoryDatasetReferenceRow{{
			AlarmType:           domain.AlarmTypeCommunity,
			ChannelID:           "UC_TEST",
			ChannelPostKey:      "UC_TEST|community:test-post",
			PostID:              "community:test-post",
			ActualPublishedAt:   &publishedAt,
			DetectedAt:          &detectedAt,
			VerificationVerdict: trackingrepo.ObservationPostComparisonVerdictMatched,
			VerificationReason:  trackingrepo.ObservationPostComparisonVerdictReasonCanonicalIdentifierMatched,
			SentCount:           1,
		}},
	}
}

func buildContinuousObservationGoldenFixture() CommunityShortsContinuousObservationReport {
	generatedAt := time.Date(2026, 4, 15, 12, 34, 56, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	observationStart := cutoverAt.Add(2 * time.Minute)
	observationEnd := observationStart.Add(24 * time.Hour)
	dataset := buildDatasetGoldenFixture()

	return CommunityShortsContinuousObservationReport{
		GeneratedAt: generatedAt,
		Observation: CommunityShortsContinuousObservationWindow{
			RuntimeName:           "youtube-scraper",
			BigBangCutoverAt:      cutoverAt,
			AppVersion:            "2.1.0",
			TargetChannelCount:    1,
			DeploymentCompletedAt: observationStart,
			ObservationStartedAt:  observationStart,
			ObservationEndsAt:     observationEnd,
			ObservedUntil:         observationEnd,
			Status:                CommunityShortsContinuousObservationStatusFinalized,
		},
		Closeout24H: CommunityShortsContinuousObservation24HCloseout{
			Status:                 CommunityShortsContinuousObservationCloseoutStatusPass,
			AggregationScope:       "all_operational_channels",
			TargetChannelCount:     1,
			ObservedPostCount:      1,
			ObservationPeriodLabel: "observation_window",
			Rule:                   "internal_system_cause_posts == 0",
			Statement:              "Finalized observation recorded internal_system_cause_posts=0.",
		},
		MissingAlarmCloseout24H: CommunityShortsContinuousObservationMissingAlarmCloseout{
			Status:             CommunityShortsContinuousObservationCloseoutStatusPass,
			AggregationScope:   "all_operational_channels",
			TargetChannelCount: 1,
			ReferencePostCount: 1,
			SendStatePostCount: 1,
			Rule:               "missing_alarm_posts == 0",
			Statement:          "Finalized observation recorded missing_alarm_posts=0.",
		},
		StateConsistencyCloseout24H: CommunityShortsContinuousObservationStateConsistencyCloseout{
			Status:             CommunityShortsContinuousObservationCloseoutStatusPass,
			AggregationScope:   "all_operational_channels",
			TargetChannelCount: 1,
			ReferencePostCount: 1,
			SendStatePostCount: 1,
			Rule:               "duplicate_sent_posts == 0 && missing_alarm_posts == 0",
			Statement:          "Finalized observation recorded duplicate_sent_posts=0 and missing_alarm_posts=0.",
		},
		TargetBaseline: communityshorts.TargetBaseline{
			GeneratedAt: generatedAt,
			Runtime: communityshorts.TargetBaselineRuntime{
				FinalDeliveryOwner:            "alarm-worker",
				CommunityShortsBigBangEnabled: true,
				TargetChannelCount:            1,
			},
			Channels: []communityshorts.TargetBaselineChannel{{
				OwnerLabel: "Member One",
				ChannelID:  "UC_TEST",
				Routes: []communityshorts.TargetBaselineChannelRoute{
					{AlarmType: domain.AlarmTypeCommunity, AlarmEnabled: true, SubscriberRoomCount: 2, EffectiveDeliveryMode: "new_only"},
					{AlarmType: domain.AlarmTypeShorts, AlarmEnabled: false, SubscriberRoomCount: 0, EffectiveDeliveryMode: "disabled"},
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
		},
		AlarmSentHistoryDataset: &dataset,
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
		},
	}
}

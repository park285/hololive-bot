package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuildCommunityShortsRouteVerificationReport(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	since := generatedAt.Add(-24 * time.Hour)

	baseline := CommunityShortsTargetBaseline{
		GeneratedAt: generatedAt,
		Runtime: CommunityShortsTargetBaselineRuntime{
			FinalDeliveryOwner:            youtubeScraperRuntimeName,
			CommunityShortsBigBangEnabled: true,
			TargetChannelCount:            3,
		},
		Channels: []CommunityShortsTargetBaselineChannel{
			{
				OwnerLabel: "A",
				ChannelID:  "UC_A",
				Routes: []CommunityShortsTargetBaselineChannelRoute{
					{
						AlarmType:             domain.AlarmTypeCommunity,
						SubscriberKey:         "alarm:subscribers:channel:community:UC_A",
						AlarmEnabled:          true,
						SubscriberRoomCount:   2,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeNew,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
					{
						AlarmType:             domain.AlarmTypeShorts,
						SubscriberKey:         "alarm:subscribers:channel:shorts:UC_A",
						AlarmEnabled:          true,
						SubscriberRoomCount:   1,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeNew,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
				},
			},
			{
				OwnerLabel: "B",
				ChannelID:  "UC_B",
				Routes: []CommunityShortsTargetBaselineChannelRoute{
					{
						AlarmType:             domain.AlarmTypeCommunity,
						SubscriberKey:         "alarm:subscribers:channel:community:UC_B",
						AlarmEnabled:          false,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeOff,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
					{
						AlarmType:             domain.AlarmTypeShorts,
						SubscriberKey:         "alarm:subscribers:channel:shorts:UC_B",
						AlarmEnabled:          true,
						SubscriberRoomCount:   1,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeNew,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
				},
			},
			{
				OwnerLabel: "C",
				ChannelID:  "UC_C",
				Routes: []CommunityShortsTargetBaselineChannelRoute{
					{
						AlarmType:             domain.AlarmTypeCommunity,
						SubscriberKey:         "alarm:subscribers:channel:community:UC_C",
						AlarmEnabled:          true,
						SubscriberRoomCount:   1,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeNew,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
					{
						AlarmType:             domain.AlarmTypeShorts,
						SubscriberKey:         "alarm:subscribers:channel:shorts:UC_C",
						AlarmEnabled:          false,
						NewPathConfigured:     true,
						EffectiveDeliveryMode: communityShortsDeliveryModeOff,
						FinalDeliveryOwner:    youtubeScraperRuntimeName,
						FinalDeliveryPath:     youtubeScraperRuntimeName + "." + communityShortsNewDeliveryPath,
					},
				},
			},
		},
	}

	pathUsage := []outbox.PostDeliveryPathUsage{
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_A",
			ContentID:         "community-a-1",
			DeliveryPath:      communityShortsNewDeliveryPath,
			ActualPublishedAt: timePtr(generatedAt.Add(-2 * time.Hour)),
			FirstSuccessAt:    timePtr(generatedAt.Add(-119 * time.Minute)),
			SuccessSendCount:  2,
			SuccessRoomCount:  2,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_A",
			ContentID:         "short-a-1",
			DeliveryPath:      communityShortsNewDeliveryPath,
			ActualPublishedAt: timePtr(generatedAt.Add(-90 * time.Minute)),
			FirstSuccessAt:    timePtr(generatedAt.Add(-80 * time.Minute)),
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
		{
			AlarmType:          domain.AlarmTypeShorts,
			ChannelID:          "UC_A",
			ContentID:          "short-a-1",
			DeliveryPath:       communityShortsLegacyDeliveryPath,
			ActualPublishedAt:  timePtr(generatedAt.Add(-90 * time.Minute)),
			FailedAttemptCount: 1,
		},
	}

	sendCounts := []outbox.PostSendCount{
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_A",
			ContentID:         "community-a-1",
			ActualPublishedAt: timePtr(generatedAt.Add(-2 * time.Hour)),
			LastSuccessAt:     timePtr(generatedAt.Add(-119 * time.Minute)),
			SuccessSendCount:  2,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_A",
			ContentID:         "short-a-1",
			ActualPublishedAt: timePtr(generatedAt.Add(-90 * time.Minute)),
			LastSuccessAt:     timePtr(generatedAt.Add(-80 * time.Minute)),
			SuccessSendCount:  1,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_B",
			ContentID:         "short-b-1",
			ActualPublishedAt: timePtr(generatedAt.Add(-30 * time.Minute)),
		},
	}

	report := BuildCommunityShortsRouteVerificationReport(baseline, pathUsage, sendCounts, generatedAt, since)
	require.Equal(t, 3, report.Summary.TargetChannelCount)
	require.Equal(t, 6, report.Summary.RouteCount)
	require.Equal(t, 4, report.Summary.ActiveRouteCount)
	require.Equal(t, 2, report.Summary.DisabledRouteCount)
	require.Equal(t, 1, report.Summary.NewOnlyUsageRouteCount)
	require.Equal(t, 1, report.Summary.NoRecentPostRouteCount)
	require.Equal(t, 1, report.Summary.NoPathObservedRouteCount)
	require.Equal(t, 1, report.Summary.MixedPathRouteCount)
	require.Equal(t, 0, report.Summary.UnexpectedPathRouteCount)

	aCommunity := reportRouteFor(t, report, "UC_A", domain.AlarmTypeCommunity)
	require.Equal(t, communityShortsDeliveryModeNew, aCommunity.ActivationState)
	require.Equal(t, communityShortsRouteUsageNewOnlyVerified, aCommunity.ActualUsageState)
	require.Equal(t, 1, aCommunity.ObservedPostCount)
	require.Equal(t, 1, aCommunity.NewPathOnlyPostCount)
	require.Equal(t, []string{communityShortsNewDeliveryPath}, aCommunity.ObservedPaths)

	aShorts := reportRouteFor(t, report, "UC_A", domain.AlarmTypeShorts)
	require.Equal(t, communityShortsRouteUsageMixedPathsDetected, aShorts.ActualUsageState)
	require.Equal(t, 1, aShorts.MixedPathPostCount)
	require.Equal(t, []string{communityShortsLegacyDeliveryPath, communityShortsNewDeliveryPath}, aShorts.ObservedPaths)

	bShorts := reportRouteFor(t, report, "UC_B", domain.AlarmTypeShorts)
	require.Equal(t, communityShortsRouteUsageNoPathObserved, bShorts.ActualUsageState)
	require.Equal(t, 1, bShorts.NoPathPostCount)
	require.Empty(t, bShorts.ObservedPaths)

	cCommunity := reportRouteFor(t, report, "UC_C", domain.AlarmTypeCommunity)
	require.Equal(t, communityShortsRouteUsageNoRecentPosts, cCommunity.ActualUsageState)
	require.Zero(t, cCommunity.ObservedPostCount)

	markdown := RenderCommunityShortsRouteVerificationMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Channel Route Verification Report")
	require.Contains(t, markdown, "runtime final owner: `youtube-scraper`")
	require.Contains(t, markdown, "A (`UC_A`)")
	require.Contains(t, markdown, "actual=`mixed_paths_detected`")
	require.Contains(t, markdown, "deployment=`youtube-scraper.youtube_outbox_dispatcher`")
}

func reportRouteFor(
	t *testing.T,
	report CommunityShortsRouteVerificationReport,
	channelID string,
	alarmType domain.AlarmType,
) CommunityShortsRouteVerificationRoute {
	t.Helper()
	for i := range report.Channels {
		if report.Channels[i].ChannelID != channelID {
			continue
		}
		for j := range report.Channels[i].Routes {
			if report.Channels[i].Routes[j].AlarmType == alarmType {
				return report.Channels[i].Routes[j]
			}
		}
	}
	t.Fatalf("route not found: channel=%s alarmType=%s", channelID, alarmType)
	return CommunityShortsRouteVerificationRoute{}
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

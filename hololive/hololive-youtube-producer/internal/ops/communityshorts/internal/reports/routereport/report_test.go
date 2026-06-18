package routereport

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	since := generatedAt.Add(-24 * time.Hour)
	baseline := testBaseline(generatedAt)
	report := Build(
		&baseline,
		testPathUsage(generatedAt),
		testSendCounts(generatedAt),
		generatedAt,
		since,
	)

	assertSummary(t, &report.Summary)
	assertRouteUsage(t, &report)
	assertMarkdown(t, &report)
}

func testBaseline(generatedAt time.Time) communityshorts.TargetBaseline {
	return communityshorts.TargetBaseline{
		GeneratedAt: generatedAt,
		Runtime: communityshorts.TargetBaselineRuntime{
			FinalDeliveryOwner:            communityshorts.RuntimeOwnerAlarmWorker,
			CommunityShortsBigBangEnabled: true,
			TargetChannelCount:            3,
		},
		Channels: []communityshorts.TargetBaselineChannel{
			testChannel("A", "UC_A", true, true),
			testChannel("B", "UC_B", false, true),
			testChannel("C", "UC_C", true, false),
		},
	}
}

func testChannel(
	ownerLabel string,
	channelID string,
	communityEnabled bool,
	shortsEnabled bool,
) communityshorts.TargetBaselineChannel {
	return communityshorts.TargetBaselineChannel{
		OwnerLabel: ownerLabel,
		ChannelID:  channelID,
		Routes: []communityshorts.TargetBaselineChannelRoute{
			testRoute(domain.AlarmTypeCommunity, channelID, communityEnabled),
			testRoute(domain.AlarmTypeShorts, channelID, shortsEnabled),
		},
	}
}

func testRoute(
	alarmType domain.AlarmType,
	channelID string,
	enabled bool,
) communityshorts.TargetBaselineChannelRoute {
	mode := communityshorts.DeliveryModeOff
	rooms := 0
	if enabled {
		mode = communityshorts.DeliveryModeNew
		rooms = 1
	}
	if channelID == "UC_A" && alarmType == domain.AlarmTypeCommunity {
		rooms = 2
	}
	return communityshorts.TargetBaselineChannelRoute{
		AlarmType:             alarmType,
		SubscriberKey:         subscriberKey(alarmType, channelID),
		AlarmEnabled:          enabled,
		SubscriberRoomCount:   rooms,
		NewPathConfigured:     true,
		EffectiveDeliveryMode: mode,
		FinalDeliveryOwner:    communityshorts.RuntimeOwnerAlarmWorker,
		FinalDeliveryPath:     communityshorts.RuntimeOwnerAlarmWorker + "." + communityshorts.NewDeliveryPath,
	}
}

func subscriberKey(alarmType domain.AlarmType, channelID string) string {
	return "alarm:subscribers:channel:" + strings.ToLower(string(alarmType)) + ":" + channelID
}

func testPathUsage(generatedAt time.Time) []outbox.PostDeliveryPathUsage {
	return []outbox.PostDeliveryPathUsage{
		{
			AlarmType:         domain.AlarmTypeCommunity,
			ChannelID:         "UC_A",
			ContentID:         "community-a-1",
			DeliveryPath:      communityshorts.NewDeliveryPath,
			ActualPublishedAt: timePtr(generatedAt.Add(-2 * time.Hour)),
			FirstSuccessAt:    timePtr(generatedAt.Add(-119 * time.Minute)),
			SuccessSendCount:  2,
			SuccessRoomCount:  2,
		},
		{
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         "UC_A",
			ContentID:         "short-a-1",
			DeliveryPath:      communityshorts.NewDeliveryPath,
			ActualPublishedAt: timePtr(generatedAt.Add(-90 * time.Minute)),
			FirstSuccessAt:    timePtr(generatedAt.Add(-80 * time.Minute)),
			SuccessSendCount:  1,
			SuccessRoomCount:  1,
		},
		testLegacyPathUsage(generatedAt),
	}
}

func testLegacyPathUsage(generatedAt time.Time) outbox.PostDeliveryPathUsage {
	return outbox.PostDeliveryPathUsage{
		AlarmType:          domain.AlarmTypeShorts,
		ChannelID:          "UC_A",
		ContentID:          "short-a-1",
		DeliveryPath:       communityshorts.LegacyDeliveryPath,
		ActualPublishedAt:  timePtr(generatedAt.Add(-90 * time.Minute)),
		FailedAttemptCount: 1,
	}
}

func testSendCounts(generatedAt time.Time) []outbox.PostSendCount {
	return []outbox.PostSendCount{
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
		testNoPathSendCount(generatedAt),
	}
}

func testNoPathSendCount(generatedAt time.Time) outbox.PostSendCount {
	return outbox.PostSendCount{
		AlarmType:         domain.AlarmTypeShorts,
		ChannelID:         "UC_B",
		ContentID:         "short-b-1",
		ActualPublishedAt: timePtr(generatedAt.Add(-30 * time.Minute)),
	}
}

func assertSummary(t *testing.T, summary *Summary) {
	t.Helper()

	require.Equal(t, 3, summary.TargetChannelCount)
	require.Equal(t, 6, summary.RouteCount)
	require.Equal(t, 4, summary.ActiveRouteCount)
	require.Equal(t, 2, summary.DisabledRouteCount)
	require.Equal(t, 1, summary.NewOnlyUsageRouteCount)
	require.Equal(t, 1, summary.NoRecentPostRouteCount)
	require.Equal(t, 1, summary.NoPathObservedRouteCount)
	require.Equal(t, 1, summary.MixedPathRouteCount)
	require.Equal(t, 0, summary.UnexpectedPathRouteCount)
}

func assertRouteUsage(t *testing.T, report *Report) {
	t.Helper()

	aCommunity := reportRouteFor(t, report, "UC_A", domain.AlarmTypeCommunity)
	require.Equal(t, communityshorts.DeliveryModeNew, aCommunity.ActivationState)
	require.Equal(t, routeUsageNewOnlyVerified, aCommunity.ActualUsageState)
	require.Equal(t, 1, aCommunity.ObservedPostCount)
	require.Equal(t, 1, aCommunity.NewPathOnlyPostCount)
	require.Equal(t, []string{communityshorts.NewDeliveryPath}, aCommunity.ObservedPaths)

	aShorts := reportRouteFor(t, report, "UC_A", domain.AlarmTypeShorts)
	require.Equal(t, routeUsageMixedPathsDetected, aShorts.ActualUsageState)
	require.Equal(t, 1, aShorts.MixedPathPostCount)
	require.Equal(t, []string{communityshorts.LegacyDeliveryPath, communityshorts.NewDeliveryPath}, aShorts.ObservedPaths)

	bShorts := reportRouteFor(t, report, "UC_B", domain.AlarmTypeShorts)
	require.Equal(t, routeUsageNoPathObserved, bShorts.ActualUsageState)
	require.Equal(t, 1, bShorts.NoPathPostCount)
	require.Empty(t, bShorts.ObservedPaths)

	cCommunity := reportRouteFor(t, report, "UC_C", domain.AlarmTypeCommunity)
	require.Equal(t, routeUsageNoRecentPosts, cCommunity.ActualUsageState)
	require.Zero(t, cCommunity.ObservedPostCount)
}

func assertMarkdown(t *testing.T, report *Report) {
	t.Helper()

	markdown := RenderMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Channel Route Verification Report")
	require.Contains(t, markdown, "runtime final owner: `alarm-worker`")
	require.Contains(t, markdown, "A (`UC_A`)")
	require.Contains(t, markdown, "actual=`mixed_paths_detected`")
	require.Contains(t, markdown, "deployment=`alarm-worker.youtube_outbox_dispatcher`")
}

func reportRouteFor(
	t *testing.T,
	report *Report,
	channelID string,
	alarmType domain.AlarmType,
) Route {
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
	return Route{}
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

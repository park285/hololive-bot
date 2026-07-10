package communityshortsops

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestChannelSummaryDispatcherWiring(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 12, 3, 0, 0, 0, time.UTC)
	report := BuildCommunityShortsChannelSummaryReport(
		[]outbox.ChannelPostDeliverySummary{{ChannelID: "UC-dispatch", DetectedPostCount: 1}},
		generatedAt,
		generatedAt.Add(-time.Hour),
	)

	require.Equal(t, int64(1), report.Summary.ChannelCount)
	require.Contains(t, RenderCommunityShortsChannelSummaryMarkdown(&report), "`UC-dispatch`")
}

func TestReportEntrypointsAreBound(t *testing.T) {
	t.Parallel()

	entrypoints := map[string]any{
		"CollectCommunityShortsChannelSummaryReport":          CollectCommunityShortsChannelSummaryReport,
		"BuildCommunityShortsChannelSummaryReport":            BuildCommunityShortsChannelSummaryReport,
		"RenderCommunityShortsChannelSummaryMarkdown":         RenderCommunityShortsChannelSummaryMarkdown,
		"BuildCommunityShortsDeliveryLogReport":               BuildCommunityShortsDeliveryLogReport,
		"RenderCommunityShortsDeliveryLogMarkdown":            RenderCommunityShortsDeliveryLogMarkdown,
		"CollectCommunityShortsDeliveryLogReport":             CollectCommunityShortsDeliveryLogReport,
		"BuildCommunityShortsLatencyCauseReport":              BuildCommunityShortsLatencyCauseReport,
		"BuildCommunityShortsLatencyCauseReportWithQuery":     BuildCommunityShortsLatencyCauseReportWithQuery,
		"RenderCommunityShortsLatencyCauseMarkdown":           RenderCommunityShortsLatencyCauseMarkdown,
		"CollectCommunityShortsLatencyCauseReport":            CollectCommunityShortsLatencyCauseReport,
		"CollectCommunityShortsLatencyCauseReportWithOptions": CollectCommunityShortsLatencyCauseReportWithOptions,
		"DefaultCommunityShortsLatencyPeriodSpecs":            DefaultCommunityShortsLatencyPeriodSpecs,
		"CollectCommunityShortsLatencyPeriodReport":           CollectCommunityShortsLatencyPeriodReport,
		"BuildCommunityShortsLatencyPeriodReport":             BuildCommunityShortsLatencyPeriodReport,
		"RenderCommunityShortsLatencyPeriodMarkdown":          RenderCommunityShortsLatencyPeriodMarkdown,
		"CollectCommunityShortsRouteVerificationReport":       CollectCommunityShortsRouteVerificationReport,
		"BuildCommunityShortsRouteVerificationReport":         BuildCommunityShortsRouteVerificationReport,
		"RenderCommunityShortsRouteVerificationMarkdown":      RenderCommunityShortsRouteVerificationMarkdown,
		"BuildCommunityShortsSendCountReport":                 BuildCommunityShortsSendCountReport,
		"BuildCommunityShortsSendCountReportWithQuery":        BuildCommunityShortsSendCountReportWithQuery,
		"RenderCommunityShortsSendCountMarkdown":              RenderCommunityShortsSendCountMarkdown,
		"CollectCommunityShortsSendCountReport":               CollectCommunityShortsSendCountReport,
		"CollectCommunityShortsSendCountReportWithOptions":    CollectCommunityShortsSendCountReportWithOptions,
	}

	for name, entrypoint := range entrypoints {
		require.NotNil(t, entrypoint, name)
	}
}

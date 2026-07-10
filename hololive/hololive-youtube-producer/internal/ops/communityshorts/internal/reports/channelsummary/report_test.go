package channelsummary

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func TestBuild_NormalizesSortsAndTotals(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	generatedAt := time.Date(2026, 4, 12, 12, 0, 0, 0, kst)
	since := generatedAt.Add(-24 * time.Hour)
	earliest := time.Date(2026, 4, 12, 8, 0, 0, 0, kst)
	latest := time.Date(2026, 4, 12, 9, 0, 0, 0, kst)
	latestRecent := latest.Add(time.Hour)
	zero := time.Time{}
	rows := []outbox.ChannelPostDeliverySummary{
		{
			ChannelID:                  " channel-b ",
			EarliestObservedAt:         &earliest,
			LatestObservedAt:           &latest,
			DetectedPostCount:          2,
			AlarmSentPostCount:         1,
			SuccessPostCount:           1,
			FailedPostCount:            1,
			DetectedUnsentPostCount:    1,
			CommunityDetectedPostCount: 2,
		},
		{
			ChannelID:               "channel-a",
			EarliestObservedAt:      &earliest,
			LatestObservedAt:        &latest,
			DetectedPostCount:       3,
			AlarmSentPostCount:      3,
			SuccessPostCount:        3,
			ShortsDetectedPostCount: 3,
		},
		{
			ChannelID:                  "channel-c",
			EarliestObservedAt:         &zero,
			LatestObservedAt:           &latestRecent,
			DetectedPostCount:          4,
			AlarmSentPostCount:         3,
			SuccessPostCount:           2,
			FailedPostCount:            1,
			CommunityDetectedPostCount: 2,
			ShortsDetectedPostCount:    2,
		},
	}

	report := Build(rows, generatedAt, since)

	require.Equal(t, generatedAt.UTC(), report.GeneratedAt)
	require.Equal(t, since.UTC(), report.WindowStart)
	require.Equal(t, generatedAt.UTC(), report.WindowEnd)
	require.Equal(t, Totals{
		ChannelCount:               3,
		DetectedPostCount:          9,
		AlarmSentPostCount:         7,
		SuccessPostCount:           6,
		FailedPostCount:            2,
		DetectedUnsentPostCount:    1,
		CommunityDetectedPostCount: 4,
		ShortsDetectedPostCount:    5,
	}, report.Summary)
	require.Equal(t, []string{"channel-c", "channel-a", "channel-b"}, []string{
		report.Rows[0].ChannelID,
		report.Rows[1].ChannelID,
		report.Rows[2].ChannelID,
	})
	require.Nil(t, report.Rows[0].EarliestObservedAt)
	require.Equal(t, latestRecent.UTC(), *report.Rows[0].LatestObservedAt)
	require.Equal(t, " channel-b ", rows[0].ChannelID, "Build must not mutate caller rows")
	require.Equal(t, kst, rows[0].LatestObservedAt.Location(), "Build must not mutate caller timestamps")

	markdown := RenderMarkdown(&report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Channel Delivery Summary")
	require.Contains(t, markdown, "`unsent_with_failures`")
	require.Contains(t, markdown, "`failures_observed`")
	require.Contains(t, markdown, "`ok`")
	require.Less(t, strings.Index(markdown, "channel-c"), strings.Index(markdown, "channel-a"))
	require.Less(t, strings.Index(markdown, "channel-a"), strings.Index(markdown, "channel-b"))
}

func TestNormalizeRequest_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 12, 3, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	validConfig := &config.Config{}

	tests := []struct {
		name      string
		ctx       context.Context
		appConfig *config.Config
		since     time.Time
		wantError string
	}{
		{name: "nil config", ctx: context.Background(), since: since, wantError: "config is nil"},
		{name: "nil context", appConfig: validConfig, since: since, wantError: "context is nil"},
		{name: "empty since", ctx: context.Background(), appConfig: validConfig, wantError: "since is empty"},
		{name: "since after now", ctx: context.Background(), appConfig: validConfig, since: now.Add(time.Second), wantError: "since is after now"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizeRequest(test.ctx, test.appConfig, nil, now, test.since)
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestRenderMarkdown_NilReport(t *testing.T) {
	t.Parallel()

	markdown := RenderMarkdown(nil)
	require.Contains(t, markdown, "generated at: `(none)`")
	require.Contains(t, markdown, "최근 윈도우에 해당하는 community/shorts 감지 채널이 없습니다.")
}

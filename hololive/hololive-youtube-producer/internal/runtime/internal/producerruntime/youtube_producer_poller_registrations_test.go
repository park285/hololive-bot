// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package producerruntime

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polltarget"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTestPoller struct {
	name string
}

func (p fakeTestPoller) Poll(context.Context, string) error { return nil }
func (p fakeTestPoller) Name() string                       { return p.name }

func newPollerRegistrationTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildYouTubeProducerChannelPollerRegistrations_DefaultOrdering(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}

	registrations := polling.BuildRegistrations(
		postgres,
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		scraper.NewRateLimiter(time.Second),
		nil,
		nil,
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_STATS_A"},
	)

	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	expected := []struct {
		name                  string
		priority              poller.Priority
		interval              time.Duration
		group                 providers.ChannelTargetGroup
		worstCaseAttempts     int
		worstCaseRequestUnits float64
	}{
		{name: "videos", priority: poller.PriorityNormal, interval: 7 * time.Minute, group: providers.ChannelTargetGroupNotification, worstCaseAttempts: scraper.FetchPageMaxAttempts, worstCaseRequestUnits: 9},
		{name: "shorts", priority: poller.PriorityLow, interval: 11 * time.Minute, group: providers.ChannelTargetGroupNotification, worstCaseAttempts: scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, worstCaseRequestUnits: 1},
		{name: "community", priority: poller.PriorityLow, interval: 11 * time.Minute, group: providers.ChannelTargetGroupNotification, worstCaseAttempts: scraper.HighFrequencyChannelFetchPolicy.MaxAttempts, worstCaseRequestUnits: 1},
		{name: "channel_stats", priority: poller.PriorityLow, interval: 4 * time.Hour, group: providers.ChannelTargetGroupStats, worstCaseAttempts: scraper.FetchPageMaxAttempts, worstCaseRequestUnits: 3},
		{name: "live", priority: poller.PriorityHigh, interval: 3 * time.Minute, group: providers.ChannelTargetGroupNotification, worstCaseAttempts: scraper.FetchPageMaxAttempts, worstCaseRequestUnits: 3},
	}

	for idx, reg := range registrations {
		if reg.Poller == nil {
			t.Fatalf("registrations[%d].Poller is nil", idx)
		}
		if reg.Poller.Name() != expected[idx].name {
			t.Fatalf("registrations[%d].Poller.Name() = %q, want %q", idx, reg.Poller.Name(), expected[idx].name)
		}
		if reg.Priority != expected[idx].priority {
			t.Fatalf("registrations[%d].Priority = %d, want %d", idx, reg.Priority, expected[idx].priority)
		}
		if reg.Interval != expected[idx].interval {
			t.Fatalf("registrations[%d].Interval = %s, want %s", idx, reg.Interval, expected[idx].interval)
		}
		if reg.TargetGroup != expected[idx].group {
			t.Fatalf("registrations[%d].TargetGroup = %q, want %q", idx, reg.TargetGroup, expected[idx].group)
		}
		if reg.RequestsPerRun != 1 {
			t.Fatalf("registrations[%d].RequestsPerRun = %d, want 1", idx, reg.RequestsPerRun)
		}
		if reg.WorstCaseAttempts != expected[idx].worstCaseAttempts {
			t.Fatalf("registrations[%d].WorstCaseAttempts = %d, want %d", idx, reg.WorstCaseAttempts, expected[idx].worstCaseAttempts)
		}
		if reg.WorstCaseRequestUnitsPerRun != expected[idx].worstCaseRequestUnits {
			t.Fatalf("registrations[%d].WorstCaseRequestUnitsPerRun = %v, want %v", idx, reg.WorstCaseRequestUnitsPerRun, expected[idx].worstCaseRequestUnits)
		}
		switch reg.Poller.Name() {
		case "channel_stats":
			if len(reg.ChannelIDs) != 1 || reg.ChannelIDs[0] != "UC_STATS_A" {
				t.Fatalf("registrations[%d].ChannelIDs = %#v, want [UC_STATS_A]", idx, reg.ChannelIDs)
			}
		default:
			if len(reg.ChannelIDs) != 2 || reg.ChannelIDs[0] != "UC_NOTIFY_A" || reg.ChannelIDs[1] != "UC_NOTIFY_B" {
				t.Fatalf("registrations[%d].ChannelIDs = %#v, want [UC_NOTIFY_A UC_NOTIFY_B]", idx, reg.ChannelIDs)
			}
		}
	}
}

func TestBuildYouTubeProducerChannelPollerRegistrations_AllExplicit(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}

	registrations := polling.BuildRegistrations(
		postgres,
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		scraper.NewRateLimiter(time.Second),
		nil,
		nil,
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_STATS_A"},
	)

	for idx, reg := range registrations {
		if reg.Poller == nil || reg.Interval <= 0 {
			continue
		}
		if !reg.HasExplicitChannelIDs {
			t.Fatalf("registrations[%d] (%s) missing explicit channel IDs", idx, reg.Poller.Name())
		}
	}
}

func TestClassifyYouTubePollTargetsByActivity(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	warmAt := now.Add(-72 * time.Hour)
	seedPollTargetLiveSession(t, pool, "active-live", "UC_LIVE", domain.LiveStatusLive, activeAt)
	seedPollTargetVideo(t, pool, "active-video", "UC_ACTIVE", activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)

	targets, err := polltarget.ClassifyByActivity(context.Background(), pool, polltarget.Targets{
		NotificationChannelIDs: []string{"UC_LIVE", "UC_ACTIVE", "UC_WARM", "UC_COLD"},
		StatsChannelIDs:        []string{"UC_STATS"},
	}, now)

	require.NoError(t, err)
	require.Equal(t, []string{"UC_LIVE", "UC_ACTIVE"}, targets.ActiveNotificationChannelIDs)
	require.Equal(t, []string{"UC_WARM"}, targets.WarmNotificationChannelIDs)
	require.Equal(t, []string{"UC_COLD"}, targets.ColdNotificationChannelIDs)
	require.Equal(t, []string{"UC_STATS"}, targets.StatsChannelIDs)
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieredTargetsReduceRPM(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	warmAt := now.Add(-72 * time.Hour)
	seedPollTargetVideo(t, pool, "active-video", "UC_ACTIVE", activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_WARM", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := config.ScraperConfig{Poll: config.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: config.ScraperPollTieringConfig{Enabled: true}}
	flatConfig := appConfig
	flatConfig.PollTiering.Enabled = false
	nilDB := &databasemocks.Client{}
	activityDB := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	flat := polling.BuildRegistrations(nilDB, flatConfig, scraper.NewRateLimiter(time.Second), nil, nil, notificationIDs, statsIDs)
	tiered := polling.BuildRegistrations(activityDB, appConfig, scraper.NewRateLimiter(time.Second), nil, nil, notificationIDs, statsIDs)

	require.Greater(t, len(tiered), len(flat))
	require.Less(t, polling.EstimateResolvedPollerRPM(tiered), polling.EstimateResolvedPollerRPM(flat))
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieringDisabledByDefault(t *testing.T) {
	appConfig := config.ScraperConfig{Poll: config.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}}
	postgres := &databasemocks.Client{}
	registrations := polling.BuildRegistrations(postgres, appConfig, scraper.NewRateLimiter(time.Second), nil, nil, []string{"UC_ACTIVE", "UC_COLD"}, []string{"UC_STATS"})

	require.False(t, polltarget.HasTieredNotificationRegistration(registrations))
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieringEnabledWithAllActiveTargets(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	seedPollTargetVideo(t, pool, "active-video", "UC_ACTIVE", activeAt, activeAt)

	appConfig := config.ScraperConfig{
		Poll: config.ScraperPoll{
			Videos:    10 * time.Minute,
			Shorts:    10 * time.Minute,
			Community: 10 * time.Minute,
			Stats:     6 * time.Hour,
			Live:      10 * time.Minute,
		},
		PollTiering: config.ScraperPollTieringConfig{Enabled: true},
	}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, appConfig, scraper.NewRateLimiter(time.Second), nil, nil, []string{"UC_ACTIVE"}, []string{"UC_STATS"})

	require.True(t, polltarget.HasTieredNotificationRegistration(registrations))
}

func TestTieredPollerRefreshPreservesTierIntervals(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	warmAt := now.Add(-72 * time.Hour)
	seedPollTargetVideo(t, pool, "active-video", "UC_ACTIVE", activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_WARM", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := config.ScraperConfig{Poll: config.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: config.ScraperPollTieringConfig{Enabled: true}}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, appConfig, scraper.NewRateLimiter(time.Second), nil, nil, notificationIDs, statsIDs)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		newPollerRegistrationTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	syncer := polltarget.NewSchedulerSyncer(scheduler, registrations, pool)

	syncer.Sync(polltarget.Targets{NotificationChannelIDs: notificationIDs, StatsChannelIDs: statsIDs})

	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_ACTIVE:videos"))
	require.Equal(t, 20*time.Minute, schedulerJobInterval(t, scheduler, "UC_WARM:videos"))
	require.Equal(t, 60*time.Minute, schedulerJobInterval(t, scheduler, "UC_COLD:videos"))
	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_ACTIVE:live"))
	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_WARM:live"))
	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_COLD:live"))
}

func TestTieredPollerRefreshRemovesEmptyNotificationTargets(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	seedPollTargetVideo(t, pool, "active-video", "UC_ACTIVE", activeAt, activeAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := config.ScraperConfig{Poll: config.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: config.ScraperPollTieringConfig{Enabled: true}}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, appConfig, scraper.NewRateLimiter(time.Second), nil, nil, notificationIDs, statsIDs)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		newPollerRegistrationTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	syncer := polltarget.NewSchedulerSyncer(scheduler, registrations, pool)

	syncer.Sync(polltarget.Targets{NotificationChannelIDs: nil, StatsChannelIDs: statsIDs})

	require.NotContains(t, schedulerJobKeys(t, scheduler), "UC_ACTIVE:videos")
	require.NotContains(t, schedulerJobKeys(t, scheduler), "UC_COLD:videos")
}

func seedPollTargetLiveSession(t *testing.T, pool *pgxpool.Pool, videoID, channelID string, status domain.LiveStatus, lastSeenAt time.Time) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_sessions(video_id, channel_id, status, title, last_seen_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (video_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    status = EXCLUDED.status,
		    last_seen_at = EXCLUDED.last_seen_at
	`, videoID, channelID, status, videoID, lastSeenAt)
	require.NoError(t, err)
}

func seedPollTargetVideo(t *testing.T, pool *pgxpool.Pool, videoID, channelID string, publishedAt, firstSeenAt time.Time) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_videos(video_id, channel_id, title, published_at, first_seen_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (video_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    published_at = EXCLUDED.published_at,
		    first_seen_at = EXCLUDED.first_seen_at,
		    last_seen_at = EXCLUDED.last_seen_at
	`, videoID, channelID, videoID, publishedAt, firstSeenAt)
	require.NoError(t, err)
}

func seedPollTargetCommunityPost(t *testing.T, pool *pgxpool.Pool, postID, channelID string, publishedAt, firstSeenAt time.Time) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_community_posts(post_id, channel_id, content_text, published_at, first_seen_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (post_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    published_at = EXCLUDED.published_at,
		    first_seen_at = EXCLUDED.first_seen_at,
		    last_seen_at = EXCLUDED.last_seen_at
	`, postID, channelID, postID, publishedAt, firstSeenAt)
	require.NoError(t, err)
}

func schedulerJobInterval(t *testing.T, scheduler any, key string) time.Duration {
	t.Helper()
	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	jobValue := field.MapIndex(reflect.ValueOf(key))
	require.True(t, jobValue.IsValid(), "job %s must exist", key)
	job := jobValue.Interface().(*poller.Job)
	return job.Interval
}

func TestValidateExplicitPollerRegistrations_ReturnsErrorOnActiveNonExplicitRegistration(t *testing.T) {
	t.Parallel()

	err := polling.ValidateExplicitPollerRegistrations([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(
			fakeTestPoller{name: "videos"},
			poller.PriorityNormal,
			time.Minute,
		),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "videos")
}

func TestBuildYouTubeProducerYouTubeComponents_GraduatedMembersFiltered(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " UCACTIVE "},
			{ChannelID: " ", Name: "missing"},
			{ChannelID: "UCGRADUATED", IsGraduated: true},
		},
	})

	scheduler, registrations, err := polling.BuildComponents(
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    5 * time.Minute,
				Shorts:    10 * time.Minute,
				Community: 10 * time.Minute,
				Stats:     6 * time.Hour,
				Live:      5 * time.Minute,
			},
		},
		postgres,
		communityshorts.EnabledChannelIDs(operationalChannels),
		communityshorts.EnabledChannelIDs(operationalChannels),
		polling.BuildSharedClient(config.ScraperConfig{}, nil, scraper.NewRateLimiter(time.Second)),
		nil,
		nil,
		nil,
		newPollerRegistrationTestLogger(),
	)
	require.NoError(t, err)

	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}
	if len(registrations) != 5 {
		t.Fatalf("len(registrations) = %d, want 5", len(registrations))
	}

	applied := scheduler.SetProxyEnabled(false)
	if applied != 5 {
		t.Fatalf("scheduler.SetProxyEnabled(false) = %d, want 5", applied)
	}
}

func TestBuildYouTubeProducerChannelPollerRegistrations_RouteAwareWorstCaseRequestUnits(t *testing.T) {
	t.Parallel()

	postgres := &databasemocks.Client{}

	registrations := polling.BuildRegistrations(
		postgres,
		config.ScraperConfig{
			Poll: config.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled: false,
			},
		},
		scraper.NewRateLimiter(time.Second),
		nil,
		func(poller.NotificationRouteRequest) bool { return true },
		[]string{"UC_NOTIFY_A"},
		[]string{"UC_STATS_A"},
	)

	byName := make(map[string]providers.ChannelPollerRegistration, len(registrations))
	for _, registration := range registrations {
		if registration.Poller == nil {
			continue
		}
		byName[registration.Poller.Name()] = registration
	}

	assert.Equal(t, 14.0, byName["shorts"].WorstCaseRequestUnitsPerRun)
	assert.Equal(t, 11.0, byName["community"].WorstCaseRequestUnitsPerRun)
}

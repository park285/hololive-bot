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

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"

	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
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

func newPollerRegistrationTestDB(t *testing.T) *databasemocks.Client {
	t.Helper()
	pool := dbtest.NewPool(t)
	return &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
}

func TestBuildYouTubeProducerChannelPollerRegistrations_DefaultOrdering(t *testing.T) {
	t.Parallel()

	postgres := newPollerRegistrationTestDB(t)

	registrations := polling.BuildRegistrations(
		postgres,
		&settings.ScraperConfig{
			Poll: settings.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		ratelimiter.New(time.Second),
		nil,
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_STATS_A"},
	)

	if len(registrations) != 1 {
		t.Fatalf("len(registrations) = %d, want 1", len(registrations))
	}

	expected := []struct {
		name                  string
		priority              pollscheduler.Priority
		interval              time.Duration
		group                 providers.ChannelTargetGroup
		worstCaseAttempts     int
		worstCaseRequestUnits float64
	}{
		{name: "channel_stats", priority: pollscheduler.PriorityLow, interval: 4 * time.Hour, group: providers.ChannelTargetGroupOperational, worstCaseAttempts: scraper.FetchPageMaxAttempts, worstCaseRequestUnits: 6},
	}

	for idx, reg := range registrations {
		assertDefaultRegistration(t, idx, &reg, expected[idx])
	}
}

func assertDefaultRegistration(t *testing.T, idx int, reg *providers.ChannelPollerRegistration, expected struct {
	name                  string
	priority              pollscheduler.Priority
	interval              time.Duration
	group                 providers.ChannelTargetGroup
	worstCaseAttempts     int
	worstCaseRequestUnits float64
}) {
	t.Helper()
	require.NotNil(t, reg.Poller, "registrations[%d].Poller", idx)
	require.Equal(t, expected.name, reg.Poller.Name(), "registrations[%d].Poller.Name()", idx)
	require.Equal(t, expected.priority, reg.Priority, "registrations[%d].Priority", idx)
	require.Equal(t, expected.interval, reg.Interval, "registrations[%d].Interval", idx)
	require.Equal(t, expected.group, reg.TargetGroup, "registrations[%d].TargetGroup", idx)
	require.Equal(t, 1, reg.RequestsPerRun, "registrations[%d].RequestsPerRun", idx)
	require.Equal(t, expected.worstCaseAttempts, reg.WorstCaseAttempts, "registrations[%d].WorstCaseAttempts", idx)
	require.Equal(t, expected.worstCaseRequestUnits, reg.WorstCaseRequestUnitsPerRun, "registrations[%d].WorstCaseRequestUnitsPerRun", idx)
	assertDefaultRegistrationChannels(t, idx, reg)
}

func assertDefaultRegistrationChannels(t *testing.T, idx int, reg *providers.ChannelPollerRegistration) {
	t.Helper()
	if reg.Poller.Name() == "channel_stats" {
		require.Equal(t, []string{"UC_STATS_A"}, reg.ChannelIDs, "registrations[%d].ChannelIDs", idx)
		return
	}
	require.Equal(t, []string{"UC_NOTIFY_A", "UC_NOTIFY_B"}, reg.ChannelIDs, "registrations[%d].ChannelIDs", idx)
}

func TestBuildYouTubeProducerChannelPollerRegistrations_AllExplicit(t *testing.T) {
	t.Parallel()

	postgres := newPollerRegistrationTestDB(t)

	registrations := polling.BuildRegistrations(
		postgres,
		&settings.ScraperConfig{
			Poll: settings.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		ratelimiter.New(time.Second),
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
	seedActivePollTargetVideo(t, pool, activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)

	targets, err := polltarget.ClassifyByActivity(context.Background(), pool, polltarget.Targets{
		NotificationChannelIDs: []string{"UC_LIVE", "UC_ACTIVE", "UC_WARM", "UC_COLD"},
		OperationalChannelIDs:  []string{"UC_STATS"},
	}, now)

	require.NoError(t, err)
	require.Equal(t, []string{"UC_LIVE", "UC_ACTIVE"}, targets.ActiveNotificationChannelIDs)
	require.Equal(t, []string{"UC_WARM"}, targets.WarmNotificationChannelIDs)
	require.Equal(t, []string{"UC_COLD"}, targets.ColdNotificationChannelIDs)
	require.Equal(t, []string{"UC_STATS"}, targets.OperationalChannelIDs)
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieredTargetsReduceRPM(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	warmAt := now.Add(-72 * time.Hour)
	seedActivePollTargetVideo(t, pool, activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_WARM", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := settings.ScraperConfig{Poll: settings.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: settings.ScraperPollTieringConfig{Enabled: true}}
	flatConfig := appConfig
	flatConfig.PollTiering.Enabled = false
	activityDB := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}

	flat := polling.BuildRegistrations(activityDB, &flatConfig, ratelimiter.New(time.Second), nil, notificationIDs, statsIDs)
	tiered := polling.BuildRegistrations(activityDB, &appConfig, ratelimiter.New(time.Second), nil, notificationIDs, statsIDs)

	require.Equal(t, len(flat), len(tiered))
	assertNoContentPollers(t, flat)
	assertNoContentPollers(t, tiered)
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieringDisabledByDefault(t *testing.T) {
	appConfig := settings.ScraperConfig{Poll: settings.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}}
	postgres := newPollerRegistrationTestDB(t)
	registrations := polling.BuildRegistrations(postgres, &appConfig, ratelimiter.New(time.Second), nil, []string{"UC_ACTIVE", "UC_COLD"}, []string{"UC_STATS"})

	require.False(t, polltarget.HasTieredNotificationRegistration(registrations))
}

func TestBuildYouTubeProducerChannelPollerRegistrations_TieringEnabledWithAllActiveTargets(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	seedActivePollTargetVideo(t, pool, activeAt, activeAt)

	appConfig := settings.ScraperConfig{
		Poll: settings.ScraperPoll{
			Videos:    10 * time.Minute,
			Shorts:    10 * time.Minute,
			Community: 10 * time.Minute,
			Stats:     6 * time.Hour,
			Live:      10 * time.Minute,
		},
		PollTiering: settings.ScraperPollTieringConfig{Enabled: true},
	}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, &appConfig, ratelimiter.New(time.Second), nil, []string{"UC_ACTIVE"}, []string{"UC_STATS"})

	require.False(t, polltarget.HasTieredNotificationRegistration(registrations))
	assertNoContentPollers(t, registrations)
}

func assertNoContentPollers(t *testing.T, registrations []providers.ChannelPollerRegistration) {
	t.Helper()
	for _, registration := range registrations {
		name := registration.Poller.Name()
		if name == "videos" || name == "shorts" || name == "shorts_backfill" {
			t.Fatalf("producer must not register %q", name)
		}
	}
}

func TestTieredPollerRefreshPreservesTierIntervals(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	warmAt := now.Add(-72 * time.Hour)
	seedActivePollTargetVideo(t, pool, activeAt, activeAt)
	seedPollTargetCommunityPost(t, pool, "warm-post", "UC_WARM", warmAt, warmAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_WARM", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := settings.ScraperConfig{Poll: settings.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: settings.ScraperPollTieringConfig{Enabled: true}}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, &appConfig, ratelimiter.New(time.Second), nil, notificationIDs, statsIDs)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		newPollerRegistrationTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	syncer := polltarget.NewSchedulerSyncer(scheduler, registrations, pool)

	syncer.SyncAt(t.Context(), polltarget.Targets{NotificationChannelIDs: notificationIDs, OperationalChannelIDs: statsIDs}, now)

	require.NotContains(t, schedulerJobKeys(t, scheduler), "UC_ACTIVE:videos")
	require.NotContains(t, schedulerJobKeys(t, scheduler), "UC_ACTIVE:live")
	require.Contains(t, schedulerJobKeys(t, scheduler), "UC_STATS:channel_stats")
}

func TestTieredPollerRefreshRemovesEmptyNotificationTargets(t *testing.T) {
	now := time.Now().UTC()
	pool := dbtest.NewPool(t)
	activeAt := now.Add(-2 * time.Hour)
	seedActivePollTargetVideo(t, pool, activeAt, activeAt)
	notificationIDs := []string{"UC_ACTIVE", "UC_COLD"}
	statsIDs := []string{"UC_STATS"}
	appConfig := settings.ScraperConfig{Poll: settings.ScraperPoll{
		Videos:    10 * time.Minute,
		Shorts:    10 * time.Minute,
		Community: 10 * time.Minute,
		Stats:     6 * time.Hour,
		Live:      10 * time.Minute,
	}, PollTiering: settings.ScraperPollTieringConfig{Enabled: true}}
	postgres := &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }}
	registrations := polling.BuildRegistrations(postgres, &appConfig, ratelimiter.New(time.Second), nil, notificationIDs, statsIDs)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		newPollerRegistrationTestLogger(),
		providers.WithChannelPollerRegistrations(registrations),
	)
	syncer := polltarget.NewSchedulerSyncer(scheduler, registrations, pool)

	syncer.SyncAt(t.Context(), polltarget.Targets{NotificationChannelIDs: nil, OperationalChannelIDs: statsIDs}, now)

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

func seedActivePollTargetVideo(t *testing.T, pool *pgxpool.Pool, publishedAt, firstSeenAt time.Time) {
	t.Helper()

	videoID := "active-video"
	channelID := "UC_ACTIVE"
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
	jobValue := field.MapIndex(reflect.ValueOf(key))
	require.True(t, jobValue.IsValid(), "job %s must exist", key)
	return time.Duration(jobValue.Elem().FieldByName("Interval").Int())
}

func TestValidateExplicitPollerRegistrations_ReturnsErrorOnActiveNonExplicitRegistration(t *testing.T) {
	t.Parallel()

	err := polling.ValidateExplicitPollerRegistrations([]providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(
			fakeTestPoller{name: "videos"},
			pollscheduler.PriorityNormal,
			time.Minute,
		),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "videos")
}

func TestBuildYouTubeProducerYouTubeComponents_GraduatedMembersFiltered(t *testing.T) {
	t.Parallel()

	postgres := newPollerRegistrationTestDB(t)

	operationalChannels := mustResolveCommunityShortsOperationalChannels(t, &fakeMemberDataProvider{
		members: []*domain.Member{
			{ChannelID: " UCACTIVE "},
			{ChannelID: " ", Name: "missing"},
			{ChannelID: "UCGRADUATED", IsGraduated: true},
		},
	})

	scheduler, registrations, err := polling.BuildComponents(
		&settings.ScraperConfig{
			Poll: settings.ScraperPoll{
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
		polling.BuildSharedClient(&settings.ScraperConfig{}, nil, ratelimiter.New(time.Second)),
		nil,
		newPollerRegistrationTestLogger(),
	)
	require.NoError(t, err)

	if scheduler == nil {
		t.Fatal("scheduler is nil")
	}
	if len(registrations) != 1 {
		t.Fatalf("len(registrations) = %d, want 1", len(registrations))
	}

	applied := scheduler.SetProxyEnabled(false)
	if applied != 1 {
		t.Fatalf("scheduler.SetProxyEnabled(false) = %d, want 1", applied)
	}
}

func TestBuildYouTubeProducerChannelPollerRegistrations_MetadataWorstCaseRequestUnits(t *testing.T) {
	t.Parallel()

	postgres := newPollerRegistrationTestDB(t)

	registrations := polling.BuildRegistrations(
		postgres,
		&settings.ScraperConfig{
			Poll: settings.ScraperPoll{
				Videos:    7 * time.Minute,
				Shorts:    11 * time.Minute,
				Community: 13 * time.Minute,
				Stats:     4 * time.Hour,
				Live:      3 * time.Minute,
			},
		},
		ratelimiter.New(time.Second),
		nil,
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

	assert.Equal(t, 6.0, byName["channel_stats"].WorstCaseRequestUnitsPerRun)
	if _, ok := byName["community"]; ok {
		t.Fatal("producer registrations must omit community poller")
	}
	if _, ok := byName["videos"]; ok {
		t.Fatal("producer registrations must omit videos poller")
	}
	if _, ok := byName["shorts"]; ok {
		t.Fatal("producer registrations must omit shorts poller")
	}
}

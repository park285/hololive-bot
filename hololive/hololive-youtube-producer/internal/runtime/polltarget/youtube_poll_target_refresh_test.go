package polltarget

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"

	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func TestYouTubePollTargetRefresherRefreshesNotificationPollersFromCache(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NEW"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_OLD"},
		[]string{"UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_STATS"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NEW", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NEW:videos")
	require.Contains(t, jobKeys, "UC_NEW:shorts")
	require.Contains(t, jobKeys, "UC_NEW:community")
	require.Contains(t, jobKeys, "UC_NEW:live")
	require.NotContains(t, jobKeys, "UC_OLD:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresherSkipsRegistryReadWhenPositiveVersionUnchanged(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	smembersCalls := 0
	cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 123
		return nil
	}
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		smembersCalls++
		return []string{"UC_VERSIONED"}, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&settings.ScraperConfig{Poll: settings.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		ratelimiter.New(time.Second),
		cache,
		[]string{"UC_OLD"},
		[]string{"UC_VERSIONED"},
	)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_VERSIONED"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_VERSIONED", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())
	refresher.refresh(context.Background())

	require.Equal(t, 1, smembersCalls)
}

func TestYouTubePollTargetRefresherRetiersWhenRegistryUnchanged(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	pool := dbtest.NewPool(t)

	cache := cachemocks.NewStrictClient()
	cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 123
		return nil
	}
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_TIER"}, nil
	}

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
	registrations := buildYouTubeProducerChannelPollerRegistrations(postgres, &appConfig, ratelimiter.New(time.Second), cache, []string{"UC_TIER"}, []string{"UC_TIER"})
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_TIER"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_TIER", Enabled: true}},
		func(context.Context) ([]string, error) { return []string{"UC_TIER"}, nil },
		newYouTubePollTargetTestLogger(),
	).withTieringDB(pool)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	require.Equal(t, time.Hour, schedulerJobInterval(t, scheduler, "UC_TIER:videos"))

	activeAt := now.Add(-2 * time.Hour)
	seedPollTargetVideo(t, pool, "active-video", "UC_TIER", activeAt, activeAt)
	now = now.Add(youtubePollTargetTieringRefreshInterval + time.Second)

	refresher.refresh(context.Background())
	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_TIER:videos"))
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

func TestYouTubePollTargetRefresherDoesNotTrustZeroRegistryVersion(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	smembersCalls := 0
	cache.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cache.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 0
		return nil
	}
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		smembersCalls++
		return []string{"UC_VERSIONED"}, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&settings.ScraperConfig{Poll: settings.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		ratelimiter.New(time.Second),
		cache,
		[]string{"UC_OLD"},
		[]string{"UC_VERSIONED"},
	)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_VERSIONED"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_VERSIONED", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())
	refresher.refresh(context.Background())

	require.Equal(t, 2, smembersCalls)
}

func TestYouTubePollTargetRefresher_SkipsImplicitGlobalRegistrationsDuringRefresh(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "global_resolver"}, pollscheduler.PriorityLow, time.Minute),
	}

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.NotContains(t, jobKeys, "UC_NOTIFY:global_resolver")
}

func TestYouTubePollTargetRefresher_PreservesExplicitGlobalRegistrationsDuringRefresh(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	const globalPollerName = "global_job"

	registrations := []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, pollscheduler.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewGlobalPollerRegistration(
			refreshTestPoller{name: globalPollerName},
			pollscheduler.PriorityLow,
			3*time.Minute,
		),
	}

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.Contains(t, jobKeys, providers.SyntheticGlobalPollerChannelID+":"+globalPollerName)
	require.NotContains(t, jobKeys, "UC_NOTIFY:"+globalPollerName)
}

func TestYouTubePollTargetRefresher_RefreshesOperationalRosterAtRuntime(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_NOTIFY", "UC_STATS_A"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS_A"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		newYouTubePollTargetTestLogger(),
	).withOperationalChannelLoader(func(context.Context) ([]communityshorts.OperationalChannel, error) {
		return []communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_B", Enabled: true},
		}, nil
	})

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.Contains(t, jobKeys, "UC_STATS_B:channel_stats")
	require.NotContains(t, jobKeys, "UC_STATS_A:channel_stats")
}

func TestYouTubePollTargetRefresherCreatesSeparatePrimaryAndBackfillJobs(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "shorts"}, pollscheduler.PriorityLow, 6*time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "shorts_backfill"}, pollscheduler.PriorityLow, 5*time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
	}
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:shorts")
	require.Contains(t, jobKeys, "UC_NOTIFY:shorts_backfill")
	require.Equal(t, 6*time.Minute, schedulerJobInterval(t, scheduler, "UC_NOTIFY:shorts"))
	require.Equal(t, 5*time.Minute, schedulerJobInterval(t, scheduler, "UC_NOTIFY:shorts_backfill"))
}

func TestYouTubePollTargetRefresher_FallsBackToLastOperationalRosterOnLoaderError(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_STATS_A"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS_A"}),
	)

	loadCalls := 0
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		newYouTubePollTargetTestLogger(),
	).withOperationalChannelLoader(func(context.Context) ([]communityshorts.OperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityshorts.OperationalChannel{
				{ChannelID: "UC_NOTIFY", Enabled: true},
				{ChannelID: "UC_STATS_B", Enabled: true},
			}, nil
		}
		return nil, assert.AnError
	})

	refresher.refresh(context.Background())
	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.Contains(t, jobKeys, "UC_STATS_B:channel_stats")
	require.NotContains(t, jobKeys, "UC_STATS_A:channel_stats")
}

func TestYouTubePollTargetRefresher_LogsOperationalFallbackOnce(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_STATS_A"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS_A"}),
	)

	logger, logBuf := newBufferedYouTubePollTargetTestLogger()
	loadCalls := 0
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		logger,
	).withOperationalChannelLoader(func(context.Context) ([]communityshorts.OperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityshorts.OperationalChannel{
				{ChannelID: "UC_NOTIFY", Enabled: true},
				{ChannelID: "UC_STATS_B", Enabled: true},
			}, nil
		}
		return nil, assert.AnError
	})

	refresher.refresh(context.Background())
	refresher.refresh(context.Background())
	refresher.refresh(context.Background())

	assert.Equal(t, 1, strings.Count(logBuf.String(), `"msg":"Using last known operational channels for YouTube poll targets"`))
}

func TestYouTubePollTargetRefresher_DoesNotLogOperationalRefreshWhenUnchanged(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_STATS_A"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS_A"}),
	)

	logger, logBuf := newBufferedYouTubePollTargetTestLogger()
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		logger,
	)

	refresher.refresh(context.Background())
	logBuf.Reset()
	refresher.refresh(context.Background())

	assert.NotContains(t, logBuf.String(), `"msg":"youtube_poll_target_operational_channels_refreshed"`)
}

func TestYouTubePollTargetRefresher_DoesNotLogOperationalRefreshWhenOnlyOrderChanges(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
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
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_STATS_A"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS_A"}),
	)

	logger, logBuf := newBufferedYouTubePollTargetTestLogger()
	loadCalls := 0
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		logger,
	).withOperationalChannelLoader(func(context.Context) ([]communityshorts.OperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityshorts.OperationalChannel{
				{ChannelID: "UC_NOTIFY", Enabled: true},
				{ChannelID: "UC_STATS_A", Enabled: true},
			}, nil
		}
		return []communityshorts.OperationalChannel{
			{ChannelID: "UC_STATS_A", Enabled: true},
			{ChannelID: "UC_NOTIFY", Enabled: true},
		}, nil
	})

	refresher.refresh(context.Background())
	logBuf.Reset()
	refresher.refresh(context.Background())

	assert.NotContains(t, logBuf.String(), `"msg":"youtube_poll_target_operational_channels_refreshed"`)
}

func TestYouTubePollTargetRefresherRefreshMetricsSuccessAndAcceptedCounts(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.ExistsFunc = func(context.Context, string) (bool, error) {
		return false, nil
	}
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_NOTIFY"}, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&settings.ScraperConfig{Poll: settings.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		ratelimiter.New(time.Second),
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_NOTIFY", "UC_STATS"},
	)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)
	refreshAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	refresher.timeNow = func() time.Time { return refreshAt }

	successBefore := testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success"))
	refresher.refresh(context.Background())

	assert.Equal(t, successBefore+1, testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success")))
	assert.Equal(t, float64(refreshAt.Unix()), testutil.ToFloat64(youtubePollTargetRefreshLastSuccessTimestamp.WithLabelValues()))
	assert.Equal(t, float64(1), testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("notification")))
	assert.Equal(t, float64(2), testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("operational")))
}

func TestYouTubePollTargetRefresherRefreshMetricsErrorPreservesLastSuccessSnapshot(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.ExistsFunc = func(context.Context, string) (bool, error) {
		return false, nil
	}
	cacheHealthy := true
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		if !cacheHealthy {
			return nil, assert.AnError
		}
		return []string{"UC_NOTIFY"}, nil
	}
	loadAlarmChannelIDs := func(context.Context) ([]string, error) {
		if !cacheHealthy {
			return nil, assert.AnError
		}
		return []string{"UC_NOTIFY"}, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&settings.ScraperConfig{Poll: settings.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		ratelimiter.New(time.Second),
		cache,
		[]string{"UC_NOTIFY"},
		[]string{"UC_NOTIFY", "UC_STATS"},
	)
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY", "UC_STATS"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityshorts.OperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		loadAlarmChannelIDs,
		newYouTubePollTargetTestLogger(),
	)
	firstRefreshAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	refresher.timeNow = func() time.Time { return firstRefreshAt }
	refresher.refresh(context.Background())

	cacheHealthy = false
	failedRefreshAt := firstRefreshAt.Add(time.Minute)
	refresher.timeNow = func() time.Time { return failedRefreshAt }
	successBefore := testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success"))
	errorBefore := testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("error"))
	lastSuccessBefore := testutil.ToFloat64(youtubePollTargetRefreshLastSuccessTimestamp.WithLabelValues())
	notificationCountBefore := testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("notification"))
	statsCountBefore := testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("operational"))

	refresher.refresh(context.Background())

	assert.Equal(t, successBefore, testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success")))
	assert.Equal(t, errorBefore+1, testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("error")))
	assert.Equal(t, lastSuccessBefore, testutil.ToFloat64(youtubePollTargetRefreshLastSuccessTimestamp.WithLabelValues()))
	assert.Equal(t, notificationCountBefore, testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("notification")))
	assert.Equal(t, statsCountBefore, testutil.ToFloat64(youtubePollTargetRefreshAcceptedTargetCount.WithLabelValues("operational")))
}

func TestDisabledYouTubePollTargetRefresherDoesNotRecordRefreshOutcome(t *testing.T) {
	refresher := newDisabledYouTubePollTargetRefresher(newYouTubePollTargetTestLogger())
	successBefore := testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success"))
	errorBefore := testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("error"))

	refresher.refresh(context.Background())

	assert.Equal(t, successBefore, testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("success")))
	assert.Equal(t, errorBefore, testutil.ToFloat64(youtubePollTargetRefreshTotal.WithLabelValues("error")))
}

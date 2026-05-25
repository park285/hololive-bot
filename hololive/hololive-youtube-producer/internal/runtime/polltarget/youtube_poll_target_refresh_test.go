package polltarget

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
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
		config.ScraperConfig{Poll: config.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		scraper.NewRateLimiter(time.Second),
		cache,
		nil,
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_VERSIONED", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())
	refresher.refresh(context.Background())

	require.Equal(t, 1, smembersCalls)
}

func TestYouTubePollTargetRefresherRetiersWhenRegistryUnchanged(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeVideo{}))

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
	postgres := &databasemocks.Client{GetGormDBFunc: func() *gorm.DB { return db }}
	registrations := buildYouTubeProducerChannelPollerRegistrations(postgres, appConfig, scraper.NewRateLimiter(time.Second), cache, nil, []string{"UC_TIER"}, []string{"UC_TIER"})
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_TIER", Enabled: true}},
		func(context.Context) ([]string, error) { return []string{"UC_TIER"}, nil },
		newYouTubePollTargetTestLogger(),
	).withTieringDB(db)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	require.Equal(t, time.Hour, schedulerJobInterval(t, scheduler, "UC_TIER:videos"))

	activeAt := now.Add(-2 * time.Hour)
	require.NoError(t, db.Create(&domain.YouTubeVideo{VideoID: "active-video", ChannelID: "UC_TIER", PublishedAt: &activeAt, FirstSeenAt: activeAt}).Error)
	now = now.Add(youtubePollTargetTieringRefreshInterval + time.Second)

	refresher.refresh(context.Background())
	require.Equal(t, 10*time.Minute, schedulerJobInterval(t, scheduler, "UC_TIER:videos"))
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
		config.ScraperConfig{Poll: config.ScraperPoll{
			Videos: 7 * time.Minute, Shorts: 11 * time.Minute, Community: 13 * time.Minute,
			Stats: 4 * time.Hour, Live: 3 * time.Minute,
		}},
		scraper.NewRateLimiter(time.Second),
		cache,
		nil,
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_VERSIONED", Enabled: true}},
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
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "global_resolver"}, poller.PriorityLow, time.Minute),
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
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

	registrations := []providers.ChannelPollerRegistration{
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "videos"}, poller.PriorityNormal, time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewGlobalPollerRegistration(
			refreshTestPoller{name: poller.PendingPublishedAtResolverPollerName},
			poller.PriorityLow,
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.Contains(t, jobKeys, providers.SyntheticGlobalPollerChannelID+":"+poller.PendingPublishedAtResolverPollerName)
	require.NotContains(t, jobKeys, "UC_NOTIFY:"+poller.PendingPublishedAtResolverPollerName)
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		newYouTubePollTargetTestLogger(),
	).withOperationalChannelLoader(func(context.Context) ([]communityShortsOperationalChannel, error) {
		return []communityShortsOperationalChannel{
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
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "shorts"}, poller.PriorityLow, 6*time.Minute).
			WithChannelIDs([]string{"UC_OLD"}).
			WithTargetGroup(providers.ChannelTargetGroupNotification),
		providers.NewChannelPollerRegistration(refreshTestPoller{name: "shorts_backfill"}, poller.PriorityLow, 5*time.Minute).
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
		[]communityShortsOperationalChannel{{ChannelID: "UC_NOTIFY", Enabled: true}},
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		newYouTubePollTargetTestLogger(),
	).withOperationalChannelLoader(func(context.Context) ([]communityShortsOperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityShortsOperationalChannel{
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		logger,
	).withOperationalChannelLoader(func(context.Context) ([]communityShortsOperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityShortsOperationalChannel{
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
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
		cache,
		nil,
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
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_NOTIFY"}, nil },
		logger,
	).withOperationalChannelLoader(func(context.Context) ([]communityShortsOperationalChannel, error) {
		loadCalls++
		if loadCalls == 1 {
			return []communityShortsOperationalChannel{
				{ChannelID: "UC_NOTIFY", Enabled: true},
				{ChannelID: "UC_STATS_A", Enabled: true},
			}, nil
		}
		return []communityShortsOperationalChannel{
			{ChannelID: "UC_STATS_A", Enabled: true},
			{ChannelID: "UC_NOTIFY", Enabled: true},
		}, nil
	})

	refresher.refresh(context.Background())
	logBuf.Reset()
	refresher.refresh(context.Background())

	assert.NotContains(t, logBuf.String(), `"msg":"youtube_poll_target_operational_channels_refreshed"`)
}

package polltarget

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestYouTubePollTargetRefresherSkipsSyncWhenResolvedTargetsAreUnchanged(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_SAME"}, nil
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
		[]string{"UC_SAME"},
		[]string{"UC_SAME", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_SAME", "UC_STATS"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_SAME", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	wakeCh := schedulerWakeCh(t, scheduler)
	drainSchedulerWakeCh(wakeCh)

	refresher.refresh(context.Background())

	requireNoSchedulerWakeSignal(t, wakeCh)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_SAME:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresherSkipsSyncWhenResolvedTargetsMatchInDifferentOrder(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_NOTIFY_B", "UC_NOTIFY_A"}, nil
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
		[]string{"UC_NOTIFY_A", "UC_NOTIFY_B"},
		[]string{"UC_NOTIFY_B", "UC_NOTIFY_A", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_NOTIFY_A", "UC_NOTIFY_B", "UC_STATS"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY_A", Enabled: true},
			{ChannelID: "UC_NOTIFY_B", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) { return nil, nil },
		newYouTubePollTargetTestLogger(),
	)

	wakeCh := schedulerWakeCh(t, scheduler)
	drainSchedulerWakeCh(wakeCh)

	refresher.refresh(context.Background())

	requireNoSchedulerWakeSignal(t, wakeCh)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY_A:videos")
	require.Contains(t, jobKeys, "UC_NOTIFY_B:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresherFallsBackToDBWhenCacheLookupFails(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return nil, assert.AnError
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
			{ChannelID: "UC_DB_ONLY", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) { return []string{"UC_DB_ONLY"}, nil },
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_DB_ONLY:videos")
	require.NotContains(t, jobKeys, "UC_OLD:videos")
}

func TestYouTubePollTargetRefresherRecentEmptyCacheKeepsPreviousResolvedTargetsDuringGrace(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{}, nil
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
	dbCalled := false
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_OLD", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			dbCalled = true
			return []string{"UC_DB_STALE"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.lastNonEmptyCacheAt = time.Now()
	refresher.timeNow = time.Now
	wakeCh := schedulerWakeCh(t, scheduler)
	drainSchedulerWakeCh(wakeCh)

	refresher.refresh(context.Background())

	assert.False(t, dbCalled)
	requireNoSchedulerWakeSignal(t, wakeCh)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_OLD:videos")
	require.NotContains(t, jobKeys, "UC_DB_STALE:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_EmptyCacheGraceStillRefreshesStatsTargets(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{}, nil
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
	dbCalled := false
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_A", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			dbCalled = true
			return []string{"UC_NOTIFY"}, nil
		},
		newYouTubePollTargetTestLogger(),
	).withOperationalChannelLoader(func(context.Context) ([]communityShortsOperationalChannel, error) {
		return []communityShortsOperationalChannel{
			{ChannelID: "UC_NOTIFY", Enabled: true},
			{ChannelID: "UC_STATS_B", Enabled: true},
		}, nil
	})
	refresher.lastNonEmptyCacheAt = time.Now()
	refresher.timeNow = time.Now

	refresher.refresh(context.Background())

	assert.False(t, dbCalled)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_NOTIFY:videos")
	require.Contains(t, jobKeys, "UC_STATS_B:channel_stats")
	require.NotContains(t, jobKeys, "UC_STATS_A:channel_stats")
}

func TestYouTubePollTargetRefresherPreservesExplicitEmptyNotificationTargets(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{}, nil
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
		nil,
		[]string{"UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_STATS"}),
	)
	dbCalled := false
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			dbCalled = true
			return []string{"UC_DB_STALE"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.lastNonEmptyCacheAt = time.Now()
	refresher.timeNow = time.Now
	wakeCh := schedulerWakeCh(t, scheduler)
	drainSchedulerWakeCh(wakeCh)

	refresher.refresh(context.Background())

	assert.False(t, dbCalled)
	requireNoSchedulerWakeSignal(t, wakeCh)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.NotContains(t, jobKeys, "UC_DB_STALE:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_PartialCacheShrinkUsesDBValidation(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A"}, nil
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
		[]string{"UC_A", "UC_B", "UC_C"},
		[]string{"UC_A", "UC_B", "UC_C", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_C", "UC_STATS"}),
	)
	dbCalls := 0
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_A", Enabled: true},
			{ChannelID: "UC_B", Enabled: true},
			{ChannelID: "UC_C", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			dbCalls++
			return []string{"UC_A", "UC_B", "UC_C"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	wakeCh := schedulerWakeCh(t, scheduler)
	drainSchedulerWakeCh(wakeCh)

	refresher.refresh(context.Background())

	require.Equal(t, 1, dbCalls)
	requireNoSchedulerWakeSignal(t, wakeCh)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.Contains(t, jobKeys, "UC_B:videos")
	require.Contains(t, jobKeys, "UC_C:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_ValidatesSameSizeSetMismatchAgainstDB(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A", "UC_C"}, nil
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
		[]string{"UC_A", "UC_B"},
		[]string{"UC_A", "UC_B", "UC_C", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_C", "UC_STATS"}),
	)
	dbCalls := 0
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_A", Enabled: true},
			{ChannelID: "UC_B", Enabled: true},
			{ChannelID: "UC_C", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			dbCalls++
			return []string{"UC_A", "UC_B"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	require.Equal(t, 1, dbCalls)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.Contains(t, jobKeys, "UC_B:videos")
	require.Contains(t, jobKeys, "UC_C:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_AllowsCacheOnlyAdditionWithinGrace(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A", "UC_B"}, nil
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
		[]string{"UC_A"},
		[]string{"UC_A", "UC_B", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_STATS"}),
	)
	now := time.Date(2026, 4, 12, 4, 0, 0, 0, time.UTC)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_A", Enabled: true},
			{ChannelID: "UC_B", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.Contains(t, jobKeys, "UC_B:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_DropsCacheOnlyAdditionAfterGraceIfStillMissingInDB(t *testing.T) {
	t.Parallel()

	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A", "UC_B"}, nil
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
		[]string{"UC_A"},
		[]string{"UC_A", "UC_B", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_STATS"}),
	)
	now := time.Date(2026, 4, 12, 4, 0, 0, 0, time.UTC)
	refresher := newYouTubePollTargetRefresher(
		cache,
		scheduler,
		registrations,
		[]communityShortsOperationalChannel{
			{ChannelID: "UC_A", Enabled: true},
			{ChannelID: "UC_B", Enabled: true},
			{ChannelID: "UC_STATS", Enabled: true},
		},
		func(context.Context) ([]string, error) {
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_B:videos")

	now = now.Add(youtubePollTargetCacheOnlyAdditionGracePeriod + time.Second)
	refresher.refresh(context.Background())
	jobKeys = schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.NotContains(t, jobKeys, "UC_B:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

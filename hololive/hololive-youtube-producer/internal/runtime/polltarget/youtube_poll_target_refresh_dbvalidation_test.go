package polltarget

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func TestYouTubePollTargetRefresher_ExpiredCacheOnlyAdditionDoesNotForceDBValidationForever(t *testing.T) {
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
		&config.ScraperConfig{
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
	dbCalls := 0
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
			dbCalls++
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	require.Equal(t, 1, dbCalls)

	now = now.Add(youtubePollTargetCacheOnlyAdditionGracePeriod + time.Second)
	refresher.refresh(context.Background())
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.NotContains(t, jobKeys, "UC_B:videos")
	validatedCalls := dbCalls

	now = now.Add(time.Second)
	refresher.refresh(context.Background())
	assert.Equal(t, validatedCalls, dbCalls)
}

func TestYouTubePollTargetRefresher_UnexpiredCacheOnlyAdditionStillForcesValidation(t *testing.T) {
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
		&config.ScraperConfig{
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
	dbCalls := 0
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
			dbCalls++
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	initialCalls := dbCalls

	now = now.Add(10 * time.Second)
	refresher.refresh(context.Background())
	assert.Greater(t, dbCalls, initialCalls)

	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_B:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_ClearsCacheOnlyStateWhenCandidateDisappears(t *testing.T) {
	t.Parallel()

	cacheResponses := [][]string{
		{"UC_A", "UC_B"},
		{"UC_A"},
		{"UC_A"},
	}
	cacheCall := 0
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			if cacheCall >= len(cacheResponses) {
				return cacheResponses[len(cacheResponses)-1], nil
			}
			response := cacheResponses[cacheCall]
			cacheCall++
			return response, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&config.ScraperConfig{
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
	dbCalls := 0
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
			dbCalls++
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)
	refresher.timeNow = func() time.Time { return now }

	refresher.refresh(context.Background())
	now = now.Add(time.Second)
	refresher.refresh(context.Background())
	now = now.Add(time.Second)
	refresher.refresh(context.Background())

	require.Equal(t, 2, dbCalls)
	require.Empty(t, refresher.cacheOnlyFirstSeen)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.NotContains(t, jobKeys, "UC_B:videos")
}

func TestYouTubePollTargetRefresher_ValidatesRemovalAgainstDB(t *testing.T) {
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
		&config.ScraperConfig{
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
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	require.Equal(t, 1, dbCalls)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.NotContains(t, jobKeys, "UC_B:videos")
	require.NotContains(t, jobKeys, "UC_C:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_DBValidationFailureKeepsPreviousTargets(t *testing.T) {
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
		&config.ScraperConfig{
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
			return nil, assert.AnError
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

func TestYouTubePollTargetRefresher_DBFallbackShrinkDoesNotRevalidate(t *testing.T) {
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
		&config.ScraperConfig{
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
			return []string{"UC_A"}, nil
		},
		newYouTubePollTargetTestLogger(),
	)

	refresher.refresh(context.Background())

	require.Equal(t, 1, dbCalls)
	jobKeys := schedulerJobKeys(t, scheduler)
	require.Contains(t, jobKeys, "UC_A:videos")
	require.NotContains(t, jobKeys, "UC_B:videos")
	require.NotContains(t, jobKeys, "UC_C:videos")
	require.Contains(t, jobKeys, "UC_STATS:channel_stats")
}

func TestYouTubePollTargetRefresher_DBValidationValidatedMetricAndLog(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&config.ScraperConfig{
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
	logger, logBuf := newBufferedYouTubePollTargetTestLogger()
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
			return []string{"UC_A", "UC_B"}, nil
		},
		logger,
	)

	before := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("validated"))
	refresher.refresh(context.Background())
	after := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("validated"))

	assert.Equal(t, float64(1), after-before)
	assert.Contains(t, logBuf.String(), `"msg":"youtube_poll_target_refresh_db_validated"`)
	assert.Contains(t, logBuf.String(), `"previous_notification_channels":3`)
	assert.Contains(t, logBuf.String(), `"candidate_notification_channels":1`)
	assert.Contains(t, logBuf.String(), `"db_notification_channels":2`)
}

func TestYouTubePollTargetRefresher_DBValidationFailureMetric(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&config.ScraperConfig{
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
		[]string{"UC_A", "UC_B", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_STATS"}),
	)
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
			return nil, assert.AnError
		},
		newYouTubePollTargetTestLogger(),
	)

	before := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("failed"))
	refresher.refresh(context.Background())
	after := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("failed"))

	assert.Equal(t, float64(1), after-before)
}

func TestYouTubePollTargetRefresher_DBValidationSkippedMetric(t *testing.T) {
	cache := cachemocks.NewStrictClient()
	cache.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		if key == "alarm:channel_registry" {
			return []string{"UC_A", "UC_B"}, nil
		}
		return nil, nil
	}

	registrations := buildYouTubeProducerChannelPollerRegistrations(
		&databasemocks.Client{},
		&config.ScraperConfig{
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
		[]string{"UC_A", "UC_B", "UC_STATS"},
	)

	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_A", "UC_B", "UC_STATS"}),
	)
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
			return nil, nil
		},
		newYouTubePollTargetTestLogger(),
	)

	before := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("skipped"))
	refresher.refresh(context.Background())
	after := testutil.ToFloat64(youtubePollTargetRefreshDBValidationTotal.WithLabelValues("skipped"))

	assert.Equal(t, float64(1), after-before)
}

package polltarget

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func newYouTubePollTargetTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBufferedYouTubePollTargetTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func schedulerJobKeys(t *testing.T, scheduler any) []string {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()

	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}

	return keys
}

func schedulerWakeCh(t *testing.T, scheduler any) chan struct{} {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName("wakeCh")
	require.True(t, field.IsValid(), "wakeCh field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()

	wakeCh, ok := field.Interface().(chan struct{})
	require.True(t, ok, "wakeCh must be chan struct{}")
	return wakeCh
}

func drainSchedulerWakeCh(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func requireNoSchedulerWakeSignal(t *testing.T, ch chan struct{}) {
	t.Helper()

	select {
	case <-ch:
		t.Fatal("expected no scheduler wake signal")
	default:
	}
}

type refreshTestPoller struct {
	name string
}

func (p refreshTestPoller) Poll(context.Context, string) error {
	return nil
}

func (p refreshTestPoller) Name() string {
	return p.name
}

func TestYouTubePollTargetRefresherRefreshesNotificationPollersFromCache(t *testing.T) {
	t.Parallel()

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	smembersCalls := 0
	cacheSvc.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cacheSvc.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 123
		return nil
	}
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cacheSvc.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 123
		return nil
	}
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryKey, key)
		return []string{"UC_TIER"}, nil
	}

	cfg := config.ScraperConfig{
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
	registrations := buildYouTubeProducerChannelPollerRegistrations(postgres, cfg, scraper.NewRateLimiter(time.Second), cacheSvc, nil, []string{"UC_TIER"}, []string{"UC_TIER"})
	scheduler := providers.ProvideScraperScheduler(
		nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		providers.WithChannelPollerRegistrations(registrations),
		providers.WithSchedulerChannelIDs([]string{"UC_TIER"}),
	)
	refresher := newYouTubePollTargetRefresher(
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	smembersCalls := 0
	cacheSvc.ExistsFunc = func(_ context.Context, key string) (bool, error) {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		return true, nil
	}
	cacheSvc.GetFunc = func(_ context.Context, key string, dest any) error {
		require.Equal(t, sharedalarmkeys.AlarmChannelRegistryVersionKey, key)
		version, ok := dest.(*int64)
		require.True(t, ok)
		*version = 0
		return nil
	}
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

func TestYouTubePollTargetRefresherSkipsSyncWhenResolvedTargetsAreUnchanged(t *testing.T) {
	t.Parallel()

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

func TestYouTubePollTargetRefresher_ExpiredCacheOnlyAdditionDoesNotForceDBValidationForever(t *testing.T) {
	t.Parallel()

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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
	cacheSvc := cachemocks.NewStrictClient()
	cacheSvc.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
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
		cacheSvc,
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
		cacheSvc,
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

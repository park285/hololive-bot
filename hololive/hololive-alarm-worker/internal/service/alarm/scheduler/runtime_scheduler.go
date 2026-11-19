package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking"
	checknotifier "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking/notifier"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	defaultYouTubeInterval = 60 * time.Second

	defaultYouTubeTimeout = 45 * time.Second
	evaluationWindowSlack = 15 * time.Second
)

type targetMinutesSource interface {
	GetTargetMinutes() []int
}

type recoveryCache interface {
	Exists(ctx context.Context, key string) (bool, error)
	WaitUntilReady(ctx context.Context, timeout time.Duration) error
}

type RuntimeScheduler struct {
	youtubeChecker checking.Runner
	notifier       checking.Sender
	cacheClient    recoveryCache

	targetMinutesSource targetMinutesSource
	alarmCacheWarmer    alarmCacheWarmer

	youtubeInterval time.Duration

	youtubeTimeout time.Duration

	logger *slog.Logger
}

type AlarmState interface {
	targetMinutesSource
	alarmCacheWarmer
}

type Cache interface {
	cache.DataCache
	cache.LowLevelCache
	WaitUntilReady(ctx context.Context, timeout time.Duration) error
}

type Dependencies struct {
	Cache          Cache
	HolodexService *holodexprovider.Service
	AlarmState     AlarmState
	Postgres       database.Client
	Notification   settings.NotificationConfig
	Outbox         dispatchoutbox.Writer
	Publish        queue.PublishConfig
	Logger         *slog.Logger
}

func NewRuntimeScheduler(deps Dependencies) (*RuntimeScheduler, error) {
	if err := validateRuntimeSchedulerDeps(deps.Cache, deps.HolodexService, deps.AlarmState); err != nil {
		return nil, fmt.Errorf("validate runtime scheduler deps: %w", err)
	}

	logger := runtimeSchedulerLogger(deps.Logger)

	youtubeInterval, youtubeEvaluationWindowCap := runtimeSchedulerYouTubeTiming(deps.Notification.CheckInterval)
	tierScheduler := tier.NewTieredScheduler(logger)
	dedupService := dedup.NewService(deps.Cache, logger)

	queuePublisher, err := queue.NewPublisher(deps.Cache, deps.Outbox, deps.Publish, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: %w", err)
	}

	youtubeChecker, err := newRuntimeSchedulerYouTubeChecker(
		deps.Cache,
		deps.HolodexService,
		tierScheduler,
		dedupService,
		youtubeEvaluationWindowCap,
		checking.NewPgYouTubeLiveSessionSource(deps.Postgres),
		deps.Postgres,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("runtime scheduler youtube checker: %w", err)
	}

	notifierService, err := checknotifier.NewNotifier(dedupService, queuePublisher, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create notifier: %w", err)
	}

	return newRuntimeSchedulerInstance(deps.Cache, deps.AlarmState, youtubeChecker, notifierService, youtubeInterval, logger), nil
}

func newRuntimeSchedulerYouTubeChecker(
	cacheClient alarmstore.SubscriberCache,
	holodexService *holodexprovider.Service,
	tierScheduler *tier.TieredScheduler,
	dedupService *dedup.Service,
	evaluationWindowCap time.Duration,
	persistedLiveSource checking.YouTubeLiveSessionSource,
	postgres database.Client,
	logger *slog.Logger,
) (*checking.YouTubeChecker, error) {
	var subscriptionDB dbx.Querier

	if postgres != nil {
		subscriptionDB = postgres.GetPool()
	}

	youtubeChecker, err := checking.NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient,
		holodexService,
		tierScheduler,
		dedupService,
		evaluationWindowCap,
		persistedLiveSource,
		subscriptionDB,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create youtube checker: %w", err)
	}

	return youtubeChecker, nil
}

func runtimeSchedulerLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}

func validateRuntimeSchedulerDeps(
	cacheClient Cache,
	holodexService *holodexprovider.Service,
	alarmState AlarmState,
) error {
	if cacheClient == nil {
		return errors.New("new runtime scheduler: cache service is nil")
	}

	if holodexService == nil {
		return errors.New("new runtime scheduler: holodex service is nil")
	}

	if alarmState == nil {
		return errors.New("new runtime scheduler: alarm CRUD is nil")
	}

	return nil
}

func runtimeSchedulerYouTubeTiming(checkInterval time.Duration) (initialDelay, interval time.Duration) {
	evaluationWindowCap := youtubeEvaluationWindowCap(checkInterval)
	if checkInterval <= 0 {
		return defaultYouTubeInterval, evaluationWindowCap
	}

	return checkInterval, evaluationWindowCap
}

func newRuntimeSchedulerInstance(
	cacheClient recoveryCache,
	alarmState AlarmState,
	youtubeChecker checking.Runner,
	notifierService checking.Sender,
	youtubeInterval time.Duration,
	logger *slog.Logger,
) *RuntimeScheduler {
	return &RuntimeScheduler{
		youtubeChecker: youtubeChecker,
		notifier:       notifierService,
		cacheClient:    cacheClient,

		targetMinutesSource: alarmState,
		alarmCacheWarmer:    alarmState,

		youtubeInterval: youtubeInterval,

		youtubeTimeout: defaultYouTubeTimeout,

		logger: logger,
	}
}

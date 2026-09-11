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

package scheduler

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking"
	chzzk2 "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking/chzzk"
	checknotifier "github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker/checking/notifier"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

const (
	defaultYouTubeInterval = 60 * time.Second
	defaultLiveInterval    = 120 * time.Second

	defaultYouTubeTimeout  = 45 * time.Second
	defaultPlatformTimeout = 30 * time.Second
	evaluationWindowSlack  = 15 * time.Second
)

type targetMinutesSource interface {
	GetTargetMinutes() []int
}

type targetMinutesUpdater interface {
	UpdateTargetMinutes([]int)
}

// RuntimeScheduler는 런타임 알람 체크 루프를 관리한다.
type RuntimeScheduler struct {
	youtubeChecker checking.Runner
	chzzkChecker   checking.Runner
	twitchChecker  checking.Runner
	notifier       checking.Sender
	cacheClient    cache.Client

	youtubeTargetUpdater  targetMinutesUpdater
	dedupTargetUpdater    targetMinutesUpdater
	targetMinutesSource   targetMinutesSource
	alarmCacheWarmer      alarmCacheWarmer
	platformMappingSyncer alarmPlatformMappingSyncer

	youtubeInterval time.Duration
	chzzkInterval   time.Duration
	twitchInterval  time.Duration

	youtubeTimeout time.Duration
	chzzkTimeout   time.Duration
	twitchTimeout  time.Duration

	logger *slog.Logger
}

// Dependencies는 모듈 내부 스케줄러 조립에 필요한 서비스와 실행 설정입니다.
// AlarmCRUD는 설정 갱신과 HTTP 경로에서 사용하는 동일 서비스여야 합니다.
type Dependencies struct {
	Cache          cache.Client
	HolodexService *holodexprovider.Service
	ChzzkClient    *chzzk.Client
	TwitchClient   *twitch.Client
	AlarmCRUD      domain.AlarmCRUD
	Postgres       database.Client
	Notification   settings.NotificationConfig
	Outbox         dispatchoutbox.Writer
	Publish        queue.PublishConfig
	TwitchEnabled  bool
	Logger         *slog.Logger
}

// NewRuntimeScheduler는 의존성을 검증하고 루프를 구성하며 실행은 시작하지 않습니다.
func NewRuntimeScheduler(deps Dependencies) (*RuntimeScheduler, error) {
	if err := validateRuntimeSchedulerDeps(deps.Cache, deps.HolodexService, deps.ChzzkClient, deps.TwitchClient, deps.AlarmCRUD); err != nil {
		return nil, fmt.Errorf("validate runtime scheduler deps: %w", err)
	}

	logger := runtimeSchedulerLogger(deps.Logger)

	targetMinutes := sharedchecker.NormalizeTargetMinutes(deps.AlarmCRUD.GetTargetMinutes())
	youtubeInterval, youtubeEvaluationWindowCap := runtimeSchedulerYouTubeTiming(deps.Notification.CheckInterval)
	tierScheduler := tier.NewTieredScheduler(logger)
	dedupService := dedup.NewService(deps.Cache, targetMinutes, logger)
	queuePublisher := newRuntimeSchedulerQueuePublisher(deps.Cache, logger, deps.Outbox, deps.Publish)

	youtubeChecker, err := newRuntimeSchedulerYouTubeChecker(
		deps.Cache,
		deps.HolodexService,
		tierScheduler,
		dedupService,
		targetMinutes,
		youtubeEvaluationWindowCap,
		checking.NewPgYouTubeLiveSessionSource(deps.Postgres),
		deps.Postgres,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("runtime scheduler youtube checker: %w", err)
	}

	chzzkChecker, err := chzzk2.NewChzzkChecker(deps.Cache, deps.ChzzkClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create chzzk checker: %w", err)
	}

	twitchResult := buildOptionalTwitchChecker(deps.Cache, deps.TwitchClient, deps.TwitchEnabled, logger)
	if twitchResult.err != nil {
		return nil, fmt.Errorf("build optional twitch checker: %w", twitchResult.err)
	}

	notifierService, err := checknotifier.NewNotifier(dedupService, queuePublisher, tierScheduler, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create notifier: %w", err)
	}

	return newRuntimeSchedulerInstance(deps.Cache, deps.AlarmCRUD, youtubeChecker, chzzkChecker, twitchResult.checker, notifierService, dedupService, youtubeInterval, logger), nil
}

type optionalTwitchCheckerResult struct {
	checker checking.Runner
	err     error
}

func buildOptionalTwitchChecker(
	cacheClient cache.Client,
	twitchClient *twitch.Client,
	enabled bool,
	logger *slog.Logger,
) optionalTwitchCheckerResult {
	if !enabled {
		logger.Info("Twitch alarm loop disabled")

		return optionalTwitchCheckerResult{}
	}

	checker, err := newTwitchChecker(cacheClient, twitchClient, logger)
	if err != nil {
		return optionalTwitchCheckerResult{err: fmt.Errorf("new twitch checker: %w", err)}
	}

	return optionalTwitchCheckerResult{checker: checker}
}

func newRuntimeSchedulerYouTubeChecker(
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	tierScheduler *tier.TieredScheduler,
	dedupService *dedup.Service,
	targetMinutes []int,
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
		targetMinutes,
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
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmCRUD domain.AlarmCRUD,
) error {
	if cacheClient == nil {
		return errors.New("new runtime scheduler: cache service is nil")
	}

	if holodexService == nil {
		return errors.New("new runtime scheduler: holodex service is nil")
	}

	if chzzkClient == nil {
		return errors.New("new runtime scheduler: chzzk client is nil")
	}

	if twitchClient == nil {
		return errors.New("new runtime scheduler: twitch client is nil")
	}

	if alarmCRUD == nil {
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

func newRuntimeSchedulerQueuePublisher(
	cacheClient cache.Client,
	logger *slog.Logger,
	outbox dispatchoutbox.Writer,
	publishConfig queue.PublishConfig,
) *queue.Publisher {
	return queue.NewPublisher(
		cacheClient,
		logger,
		queue.WithOutbox(outbox),
		queue.WithWakeupEnabled(publishConfig.WakeupEnabled),
		queue.WithMaxDeliveriesPerBatch(publishConfig.MaxDeliveriesPerBatch),
	)
}

func newRuntimeSchedulerInstance(
	cacheClient cache.Client,
	alarmCRUD domain.AlarmCRUD,
	youtubeChecker *checking.YouTubeChecker,
	chzzkChecker checking.Runner,
	twitchChecker checking.Runner,
	notifierService checking.Sender,
	dedupService targetMinutesUpdater,
	youtubeInterval time.Duration,
	logger *slog.Logger,
) *RuntimeScheduler {
	return &RuntimeScheduler{
		youtubeChecker: youtubeChecker,
		chzzkChecker:   chzzkChecker,
		twitchChecker:  twitchChecker,
		notifier:       notifierService,
		cacheClient:    cacheClient,

		youtubeTargetUpdater:  youtubeChecker,
		dedupTargetUpdater:    dedupService,
		targetMinutesSource:   alarmCRUD,
		alarmCacheWarmer:      alarmCRUD,
		platformMappingSyncer: alarmPlatformMappingSyncerFrom(alarmCRUD),

		youtubeInterval: youtubeInterval,
		chzzkInterval:   defaultLiveInterval,
		twitchInterval:  defaultLiveInterval,

		youtubeTimeout: defaultYouTubeTimeout,
		chzzkTimeout:   defaultPlatformTimeout,
		twitchTimeout:  defaultPlatformTimeout,

		logger: logger,
	}
}

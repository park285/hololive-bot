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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
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
	youtubeChecker checker.Runner
	chzzkChecker   checker.Runner
	twitchChecker  checker.Runner
	notifier       checker.Sender
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

// NewRuntimeScheduler는 런타임 알람 스케줄러를 생성한다.
func NewRuntimeScheduler(
	cacheClient cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmCRUD domain.AlarmCRUD,
	postgres database.Client,
	notifConfig config.NotificationConfig,
	outbox dispatchoutbox.Writer,
	publishConfig queue.PublishConfig,
	twitchEnabled bool,
	logger *slog.Logger,
) (*RuntimeScheduler, error) {
	if err := validateRuntimeSchedulerDeps(cacheClient, holodexSvc, chzzkClient, twitchClient, alarmCRUD); err != nil {
		return nil, err
	}

	logger = runtimeSchedulerLogger(logger)

	targetMinutes := sharedchecker.NormalizeTargetMinutes(alarmCRUD.GetTargetMinutes())
	youtubeInterval, youtubeEvaluationWindowCap := runtimeSchedulerYouTubeTiming(notifConfig.CheckInterval)
	tierScheduler := tier.NewTieredScheduler(logger)
	dedupSvc := dedup.NewService(cacheClient, targetMinutes, logger)
	queuePublisher := newRuntimeSchedulerQueuePublisher(cacheClient, logger, outbox, publishConfig)

	youtubeChecker, err := newRuntimeSchedulerYouTubeChecker(
		cacheClient,
		holodexSvc,
		tierScheduler,
		dedupSvc,
		targetMinutes,
		youtubeEvaluationWindowCap,
		checker.NewPgYouTubeLiveSessionSource(postgres),
		logger,
	)
	if err != nil {
		return nil, err
	}

	chzzkChecker, err := checker.NewChzzkChecker(cacheClient, chzzkClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create chzzk checker: %w", err)
	}

	twitchChecker, err := newOptionalTwitchChecker(cacheClient, twitchClient, twitchEnabled, logger)
	if err != nil {
		return nil, err
	}
	notifierSvc, err := checker.NewNotifier(dedupSvc, queuePublisher, tierScheduler, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create notifier: %w", err)
	}

	return newRuntimeSchedulerInstance(cacheClient, alarmCRUD, youtubeChecker, chzzkChecker, twitchChecker, notifierSvc, dedupSvc, youtubeInterval, logger), nil
}

func newRuntimeSchedulerYouTubeChecker(
	cacheClient cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	persistedLiveSource checker.YouTubeLiveSessionSource,
	logger *slog.Logger,
) (*checker.YouTubeChecker, error) {
	youtubeChecker, err := checker.NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient,
		holodexSvc,
		tierScheduler,
		dedupSvc,
		targetMinutes,
		evaluationWindowCap,
		persistedLiveSource,
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
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	alarmCRUD domain.AlarmCRUD,
) error {
	if cacheClient == nil {
		return errors.New("new runtime scheduler: cache service is nil")
	}

	if holodexSvc == nil {
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

func runtimeSchedulerYouTubeTiming(checkInterval time.Duration) (time.Duration, time.Duration) {
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
		queue.WithPublishMode(publishConfig.Mode),
		queue.WithShadowFatal(publishConfig.ShadowFatal),
		queue.WithWakeupEnabled(publishConfig.WakeupEnabled),
		queue.WithMaxDeliveriesPerBatch(publishConfig.MaxDeliveriesPerBatch),
	)
}

func newRuntimeSchedulerInstance(
	cacheClient cache.Client,
	alarmCRUD domain.AlarmCRUD,
	youtubeChecker *checker.YouTubeChecker,
	chzzkChecker checker.Runner,
	twitchChecker checker.Runner,
	notifierSvc checker.Sender,
	dedupSvc targetMinutesUpdater,
	youtubeInterval time.Duration,
	logger *slog.Logger,
) *RuntimeScheduler {
	return &RuntimeScheduler{
		youtubeChecker: youtubeChecker,
		chzzkChecker:   chzzkChecker,
		twitchChecker:  twitchChecker,
		notifier:       notifierSvc,
		cacheClient:    cacheClient,

		youtubeTargetUpdater:  youtubeChecker,
		dedupTargetUpdater:    dedupSvc,
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

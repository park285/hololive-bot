package checking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	channelProcessingConcurrency = 16

	youtubeHolodexLiveStatusBudget = 25 * time.Second
)

type YouTubeChecker struct {
	cacheClient         alarmstore.SubscriberCache
	holodexService      *holodexprovider.Service
	holodexBudget       time.Duration
	tierScheduler       *tier.TieredScheduler
	dedupService        *dedup.Service
	persistedLiveSource YouTubeLiveSessionSource
	subscribers         *alarmstore.SubscriberResolver
	pendingSubscribers  *alarmstore.SubscriberResolver
	lookupSubscribers   func(context.Context, string, string, domain.AlarmType) ([]string, error)
	upcomingCandidates  dispatchoutbox.UpcomingCandidateStore
	unstagedMu          sync.Mutex
	unstagedUpcoming    map[string]*unstagedYouTubeCandidates
	evaluationWindowCap time.Duration
	logger              *slog.Logger
}

func NewYouTubeCheckerWithPersistedLiveSource(
	cacheClient alarmstore.SubscriberCache,
	holodexService *holodexprovider.Service,
	tierScheduler *tier.TieredScheduler,
	dedupService *dedup.Service,
	evaluationWindowCap time.Duration,
	persistedLiveSource YouTubeLiveSessionSource,
	subscriptionDB dbx.Querier,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	if cacheClient == nil {
		return nil, errors.New("new youtube checker: cache service is nil")
	}

	if holodexService == nil {
		return nil, errors.New("new youtube checker: holodex service is nil")
	}

	if tierScheduler == nil {
		return nil, errors.New("new youtube checker: tier scheduler is nil")
	}

	if dedupService == nil {
		return nil, errors.New("new youtube checker: dedup service is nil")
	}

	if evaluationWindowCap <= 0 {
		evaluationWindowCap = 75 * time.Second
	}

	checkerMetrics()

	subscribers := alarmstore.NewSubscriberResolver(cacheClient, subscriptionDB)
	checker := &YouTubeChecker{
		cacheClient:         cacheClient,
		holodexService:      holodexService,
		holodexBudget:       youtubeHolodexLiveStatusBudget,
		tierScheduler:       tierScheduler,
		dedupService:        dedupService,
		persistedLiveSource: persistedLiveSource,
		subscribers:         subscribers,
		pendingSubscribers:  alarmstore.NewSubscriberResolver(nil, subscriptionDB),
		lookupSubscribers:   subscribers.ResolveEventSubscribers,
		evaluationWindowCap: evaluationWindowCap,
		logger:              SafeLogger(logger),
	}

	if subscriptionDB != nil {
		checker.upcomingCandidates = dispatchoutbox.NewUpcomingCandidates(subscriptionDB)
	}

	return checker, nil
}

func (c *YouTubeChecker) Check(ctx context.Context, targetPolicy targetpolicy.TargetMinutePolicy) ([]*domain.AlarmNotification, error) {
	now := time.Now().UTC()

	if err := c.flushUnstagedUpcoming(ctx); err != nil {
		return c.recoverUpcomingAfterFailure(ctx, nil, now, fmt.Errorf("flush unstaged upcoming: %w", err))
	}

	dueChannels, streamsByChannel, liveEvidence, subscriberMap, err := c.loadDueYouTubeCheckInputs(ctx, now)
	if err != nil {
		return c.recoverUpcomingAfterFailure(ctx, liveEvidence.currentProviderStreams, now, fmt.Errorf("load due youtube check inputs: %w", err))
	}

	out, err := c.collectDueYouTubeNotifications(
		ctx,
		targetPolicy,
		dueChannels,
		streamsByChannel,
		liveEvidence.observedAtByStreamID,
		subscriberMap,
		liveEvidence.settledRoomsByStreamID,
		now,
	)
	if err != nil {
		return c.recoverUpcomingAfterFailure(ctx, liveEvidence.currentProviderStreams, now, fmt.Errorf("collect due youtube notifications: %w", err))
	}

	if c.upcomingCandidates != nil {
		out = withoutUpcomingNotifications(out)

		recovered, recoverErr := c.recoverUpcomingCandidates(ctx, liveEvidence.currentProviderStreams, now)
		if recoverErr != nil {
			return out, fmt.Errorf("recover upcoming candidates: %w", recoverErr)
		}

		out = append(out, recovered...)
	}

	return out, nil
}

func (c *YouTubeChecker) collectDueYouTubeNotifications(
	ctx context.Context,
	targetPolicy targetpolicy.TargetMinutePolicy,
	dueChannels []string,
	streamsByChannel map[string][]*domain.Stream,
	liveObservedAtByStreamID map[string]time.Time,
	subscriberMap map[string][]string,
	settledRoomsByStreamID map[string]map[string]struct{},
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(dueChannels)*5)

	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(channelProcessingConcurrency)

	for _, channelID := range dueChannels {
		work, ok := c.prepareYouTubeChannelWork(channelID, streamsByChannel, subscriberMap, now)
		if !ok {
			continue
		}

		c.startYouTubeChannelWorker(egCtx, eg, targetPolicy, &work, liveObservedAtByStreamID, settledRoomsByStreamID, now, &mu, &notifications)
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("check youtube streams: wait channel workers: %w", err)
	}

	return notifications, nil
}

func (c *YouTubeChecker) startYouTubeChannelWorker(
	ctx context.Context,
	eg *errgroup.Group,
	targetPolicy targetpolicy.TargetMinutePolicy,
	work *youtubeChannelCheckWork,
	liveObservedAtByStreamID map[string]time.Time,
	settledRoomsByStreamID map[string]map[string]struct{},
	now time.Time,
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
) {
	eg.Go(func() error {
		return panicguard.RunE(c.logger, panicscope.BackgroundTask, "youtube-channel-check", func() error {
			channelNotifications, err := c.buildChannelNotifications(
				ctx,
				targetPolicy,
				work.channelID,
				work.subscriberRooms,
				work.streams,
				work.window,
				now,
				settledRoomsByStreamID,
				liveObservedAtByStreamID,
			)
			if err != nil {
				return fmt.Errorf("build channel notifications for %s: %w", work.channelID, err)
			}

			if err := c.stageUpcomingChannel(ctx, work, now, channelNotifications); err != nil {
				return fmt.Errorf("stage selected candidates: %w", err)
			}

			c.tierScheduler.UpdateChannelState(now, work.channelID, work.streams)

			appendYouTubeChannelNotifications(mu, notifications, channelNotifications)

			return nil
		})
	})
}

type youtubeChannelCheckWork struct {
	channelID       string
	streams         []*domain.Stream
	subscriberRooms []string
	window          targetpolicy.EvaluationWindow
}

func (c *YouTubeChecker) prepareYouTubeChannelWork(
	channelID string,
	streamsByChannel map[string][]*domain.Stream,
	subscriberMap map[string][]string,
	now time.Time,
) (youtubeChannelCheckWork, bool) {
	channelStreams := streamsByChannel[channelID]
	if len(channelStreams) == 0 {
		channelStreams = []*domain.Stream{}
	}

	prevCheckedAt := c.tierScheduler.LastCheckedAt(channelID)
	subscriberRooms := subscriberMap[channelID]

	if len(subscriberRooms) == 0 {
		c.tierScheduler.UpdateChannelState(now, channelID, channelStreams)

		return youtubeChannelCheckWork{}, false
	}

	return youtubeChannelCheckWork{
		channelID:       channelID,
		streams:         channelStreams,
		subscriberRooms: subscriberRooms,
		window:          targetpolicy.ResolveEvaluationWindow(prevCheckedAt, now, c.evaluationWindowCap),
	}, true
}

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

package checking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"golang.org/x/sync/errgroup"
)

const (
	channelProcessingConcurrency = 16
)

// YouTubeChecker는 Holodex live status 기반 알림 후보를 생성한다.
type YouTubeChecker struct {
	cacheClient         cache.Client
	holodexSvc          *holodex.Service
	tierScheduler       *tier.TieredScheduler
	dedupSvc            *dedup.Service
	persistedLiveSource YouTubeLiveSessionSource
	targetPolicy        sharedchecker.TargetMinutePolicy
	targetMinutesMu     sync.RWMutex
	evaluationWindowCap time.Duration
	logger              *slog.Logger
}

// NewYouTubeChecker는 YouTube 체커를 생성한다.
func NewYouTubeChecker(
	cacheClient cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	return NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient,
		holodexSvc,
		tierScheduler,
		dedupSvc,
		targetMinutes,
		evaluationWindowCap,
		nil,
		logger,
	)
}

func NewYouTubeCheckerWithPersistedLiveSource(
	cacheClient cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	persistedLiveSource YouTubeLiveSessionSource,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	if cacheClient == nil {
		return nil, errors.New("new youtube checker: cache service is nil")
	}

	if holodexSvc == nil {
		return nil, errors.New("new youtube checker: holodex service is nil")
	}

	if tierScheduler == nil {
		return nil, errors.New("new youtube checker: tier scheduler is nil")
	}

	if dedupSvc == nil {
		return nil, errors.New("new youtube checker: dedup service is nil")
	}

	if evaluationWindowCap <= 0 {
		evaluationWindowCap = 75 * time.Second
	}

	initCheckerMetrics()

	return &YouTubeChecker{
		cacheClient:         cacheClient,
		holodexSvc:          holodexSvc,
		tierScheduler:       tierScheduler,
		dedupSvc:            dedupSvc,
		persistedLiveSource: persistedLiveSource,
		targetPolicy:        sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes)),
		evaluationWindowCap: evaluationWindowCap,
		logger:              safeLogger(logger),
	}, nil
}

// UpdateTargetMinutes는 runtime 설정 변경 시 target minute 정책을 갱신한다.
func (c *YouTubeChecker) UpdateTargetMinutes(targetMinutes []int) {
	c.targetMinutesMu.Lock()
	defer c.targetMinutesMu.Unlock()

	c.targetPolicy = sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes))
}

// Check는 upcoming/live-catchup 알림 후보를 생성한다.
func (c *YouTubeChecker) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	now := time.Now().UTC()
	dueChannels, streamsByChannel, liveObservedAtByStreamID, subscriberMap, err := c.loadDueYouTubeCheckInputs(ctx, now)
	if err != nil {
		return nil, err
	}

	if len(dueChannels) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	return c.collectDueYouTubeNotifications(ctx, dueChannels, streamsByChannel, liveObservedAtByStreamID, subscriberMap, now)
}

func (c *YouTubeChecker) collectDueYouTubeNotifications(
	ctx context.Context,
	dueChannels []string,
	streamsByChannel map[string][]*domain.Stream,
	liveObservedAtByStreamID map[string]time.Time,
	subscriberMap map[string][]string,
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
		c.startYouTubeChannelWorker(eg, egCtx, work, liveObservedAtByStreamID, now, &mu, &notifications)
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("check youtube streams: wait channel workers: %w", err)
	}

	return notifications, nil
}

func (c *YouTubeChecker) startYouTubeChannelWorker(
	eg *errgroup.Group,
	ctx context.Context,
	work youtubeChannelCheckWork,
	liveObservedAtByStreamID map[string]time.Time,
	now time.Time,
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
) {
	eg.Go(func() error {
		channelNotifications, err := c.buildChannelNotifications(
			ctx,
			work.channelID,
			work.subscriberRooms,
			work.streams,
			work.window,
			now,
			liveObservedAtByStreamID,
		)
		if err != nil {
			return fmt.Errorf("check youtube streams: build channel notifications for %s: %w", work.channelID, err)
		}
		appendYouTubeChannelNotifications(mu, notifications, channelNotifications)
		return nil
	})
}

type youtubeChannelCheckWork struct {
	channelID       string
	streams         []*domain.Stream
	subscriberRooms []string
	window          sharedchecker.EvaluationWindow
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
	c.tierScheduler.UpdateChannelState(channelID, channelStreams)

	subscriberRooms := subscriberMap[channelID]
	if len(subscriberRooms) == 0 {
		return youtubeChannelCheckWork{}, false
	}

	return youtubeChannelCheckWork{
		channelID:       channelID,
		streams:         channelStreams,
		subscriberRooms: subscriberRooms,
		window:          sharedchecker.ResolveEvaluationWindow(prevCheckedAt, now, c.evaluationWindowCap),
	}, true
}

func (c *YouTubeChecker) targetMinutesSnapshot() []int {
	return c.targetPolicySnapshot().Clone()
}

func (c *YouTubeChecker) targetPolicySnapshot() sharedchecker.TargetMinutePolicy {
	c.targetMinutesMu.RLock()
	defer c.targetMinutesMu.RUnlock()

	return c.targetPolicy
}

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

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

const (
	channelProcessingConcurrency = 16

	// Holodex live 조회가 check 주기(scheduler 45초) 안에서 쓸 수 있는 최대 시간이다.
	// Holodex client는 시도당 20초로 최대 4회 시도하므로 한도가 없으면 주기 예산을 모두 쓰고, 그 뒤의 저장 세션 조회와
	// 구독·이름 조회가 기한 초과로 실패한다. 실제로 2026-09-30 Holodex 장애에서 108회 중 101회가 주기 실패였고 그중 54회가
	// 저장 세션 조회 기한 초과였다. 시도 1회는 끝나게 두고 나머지 20초를 persisted live session 예외 계약
	// (contracts/alarm.md) 경로에 남긴다.
	youtubeHolodexLiveStatusBudget = 25 * time.Second
)

// YouTubeChecker는 Holodex live status 기반 알림 후보를 생성한다.
type YouTubeChecker struct {
	cacheClient         cache.Client
	holodexService      *holodexprovider.Service
	holodexBudget       time.Duration
	tierScheduler       *tier.TieredScheduler
	dedupService        *dedup.Service
	persistedLiveSource YouTubeLiveSessionSource
	subscriptionDB      dbx.Querier
	lookupSubscribers   func(context.Context, string, string, domain.AlarmType) ([]string, error)
	upcomingCandidates  dispatchoutbox.UpcomingCandidateStore
	unstagedMu          sync.Mutex
	unstagedUpcoming    map[string]*unstagedYouTubeCandidates
	targetPolicy        sharedchecker.TargetMinutePolicy
	targetMinutesMu     sync.RWMutex
	evaluationWindowCap time.Duration
	logger              *slog.Logger
}

// NewYouTubeChecker는 YouTube 체커를 생성한다.
func NewYouTubeChecker(
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	tierScheduler *tier.TieredScheduler,
	dedupService *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	out, err := NewYouTubeCheckerWithPersistedLiveSource(
		cacheClient,
		holodexService,
		tierScheduler,
		dedupService,
		targetMinutes,
		evaluationWindowCap,
		nil,
		nil,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("youtube checker with persisted live source: %w", err)
	}

	return out, nil
}

// NewYouTubeCheckerWithPersistedLiveSource는 저장된 방송 근거와 구독 DB를 사용하는 체커를 생성한다.
// 구독 DB인 subscriptionDB가 없으면 UNIT B 멤버별 대상 선정과 구독 cache가 비어 있는 채널의 구독 확정은 오류로 처리한다.
func NewYouTubeCheckerWithPersistedLiveSource(
	cacheClient cache.Client,
	holodexService *holodexprovider.Service,
	tierScheduler *tier.TieredScheduler,
	dedupService *dedup.Service,
	targetMinutes []int,
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

	initCheckerMetrics()

	checker := &YouTubeChecker{
		cacheClient:         cacheClient,
		holodexService:      holodexService,
		holodexBudget:       youtubeHolodexLiveStatusBudget,
		tierScheduler:       tierScheduler,
		dedupService:        dedupService,
		persistedLiveSource: persistedLiveSource,
		subscriptionDB:      subscriptionDB,
		lookupSubscribers: func(ctx context.Context, channelID, title string, alarmType domain.AlarmType) ([]string, error) {
			return sharedalarm.ResolveEventSubscribers(ctx, cacheClient, subscriptionDB, channelID, title, alarmType)
		},
		targetPolicy:        sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes)),
		evaluationWindowCap: evaluationWindowCap,
		logger:              SafeLogger(logger),
	}
	if subscriptionDB != nil {
		checker.upcomingCandidates = dispatchoutbox.NewUpcomingCandidates(subscriptionDB)
	}

	return checker, nil
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

	if err := c.flushUnstagedUpcoming(ctx); err != nil {
		return c.recoverUpcomingAfterFailure(ctx, nil, now, fmt.Errorf("flush unstaged upcoming: %w", err))
	}

	dueChannels, streamsByChannel, liveEvidence, subscriberMap, err := c.loadDueYouTubeCheckInputs(ctx, now)
	if err != nil {
		return c.recoverUpcomingAfterFailure(ctx, liveEvidence.currentProviderStreams, now, fmt.Errorf("load due youtube check inputs: %w", err))
	}

	out, err := c.collectDueYouTubeNotifications(
		ctx,
		dueChannels,
		streamsByChannel,
		liveEvidence.observedAtByStreamID,
		subscriberMap,
		liveEvidence.sentRoomsByStreamID,
		now,
	)
	if err != nil {
		return c.recoverUpcomingAfterFailure(ctx, liveEvidence.currentProviderStreams, now, fmt.Errorf("collect due youtube notifications: %w", err))
	}

	if c.upcomingCandidates != nil {
		// 새 후보도 저장된 snapshot으로 반환해 동일 key의 재평가가 본문을 바꾸지 않는다.
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
	dueChannels []string,
	streamsByChannel map[string][]*domain.Stream,
	liveObservedAtByStreamID map[string]time.Time,
	subscriberMap map[string][]string,
	sentRoomsByStreamID map[string]map[string]struct{},
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

		c.startYouTubeChannelWorker(egCtx, eg, &work, liveObservedAtByStreamID, sentRoomsByStreamID, now, &mu, &notifications)
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("check youtube streams: wait channel workers: %w", err)
	}

	return notifications, nil
}

func (c *YouTubeChecker) startYouTubeChannelWorker(
	ctx context.Context,
	eg *errgroup.Group,
	work *youtubeChannelCheckWork,
	liveObservedAtByStreamID map[string]time.Time,
	sentRoomsByStreamID map[string]map[string]struct{},
	now time.Time,
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
) {
	eg.Go(func() error {
		return panicguard.RunE(c.logger, panicguard.BackgroundTask, "youtube-channel-check", func() error {
			channelNotifications, err := c.buildChannelNotifications(
				ctx,
				work.channelID,
				work.subscriberRooms,
				work.streams,
				work.window,
				now,
				sentRoomsByStreamID,
				liveObservedAtByStreamID,
			)
			if err != nil {
				return fmt.Errorf("check youtube streams: build channel notifications for %s: %w", work.channelID, err)
			}

			if err := c.stageUpcomingChannel(ctx, work, now, channelNotifications); err != nil {
				return fmt.Errorf("check youtube streams: stage selected candidates: %w", err)
			}

			// 조회 시각은 선정 후보의 durable commit 이후에만 평가 완료로 인정한다.
			c.tierScheduler.UpdateChannelState(work.channelID, work.streams)

			appendYouTubeChannelNotifications(mu, notifications, channelNotifications)

			return nil
		})
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
	subscriberRooms := subscriberMap[channelID]

	if len(subscriberRooms) == 0 {
		c.tierScheduler.UpdateChannelState(channelID, channelStreams)

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

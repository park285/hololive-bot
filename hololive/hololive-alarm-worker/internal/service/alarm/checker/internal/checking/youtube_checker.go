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
	cacheSvc            cache.Client
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
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	return NewYouTubeCheckerWithPersistedLiveSource(
		cacheSvc,
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
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	evaluationWindowCap time.Duration,
	persistedLiveSource YouTubeLiveSessionSource,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	if cacheSvc == nil {
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
		cacheSvc:            cacheSvc,
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

func appendYouTubeChannelNotifications(
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
	channelNotifications []*domain.AlarmNotification,
) {
	if len(channelNotifications) == 0 {
		return
	}

	mu.Lock()
	*notifications = append(*notifications, channelNotifications...)
	mu.Unlock()
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

func (c *YouTubeChecker) buildChannelNotifications(
	ctx context.Context,
	channelID string,
	subscriberRooms []string,
	streams []*domain.Stream,
	window sharedchecker.EvaluationWindow,
	now time.Time,
	liveObservedAtByStreamID ...map[string]time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(streams)*len(subscriberRooms))
	for _, stream := range streams {
		if stream == nil {
			continue
		}

		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, stream, subscriberRooms, window)
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build upcoming notifications: %w", err)
		}

		notifications = append(notifications, upcomingNotifications...)

		liveCatchupNotifications, err := c.buildLiveCatchupNotifications(ctx, channelID, stream, subscriberRooms, now, liveObservedAt(stream, liveObservedAtByStreamID...))
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build live catchup notifications: %w", err)
		}

		notifications = append(notifications, liveCatchupNotifications...)
	}

	return notifications, nil
}

func (c *YouTubeChecker) buildUpcomingNotifications(
	ctx context.Context,
	stream *domain.Stream,
	subscriberRooms []string,
	window sharedchecker.EvaluationWindow,
) ([]*domain.AlarmNotification, error) {
	if !isUpcomingNotificationCandidate(stream, window) {
		return nil, nil
	}

	selection, err := c.resolveYouTubeUpcomingSelection(ctx, stream, subscriberRooms, window)
	if err != nil {
		return nil, err
	}

	if !selection.selected {
		return nil, nil
	}

	alreadyNotified, err := c.dedupSvc.IsAlreadyNotifiedForSchedule(ctx, stream.ID, *stream.StartScheduled, selection.minutesUntil)
	if err != nil {
		return nil, fmt.Errorf("build upcoming notifications: check already notified for schedule: %w", err)
	}

	if alreadyNotified {
		observeYouTubeUpcomingDecision("already_notified", selection.minutesUntil, selection.label, window)
		return nil, nil
	}

	notifications := buildYouTubeUpcomingRoomNotifications(stream, subscriberRooms, selection)

	observeYouTubeUpcomingDecision("selected", selection.minutesUntil, selection.label, window)
	c.logYouTubeUpcomingSelection(stream, selection, window, len(notifications))

	return notifications, nil
}

func isUpcomingNotificationCandidate(stream *domain.Stream, window sharedchecker.EvaluationWindow) bool {
	return stream != nil && stream.IsUpcoming() && stream.StartScheduled != nil && stream.StartScheduled.After(window.End)
}

type youtubeUpcomingSelection struct {
	currentMinutesUntil  int
	previousMinutesUntil int
	minutesUntil         int
	targetCrossed        bool
	scheduleChanges      map[string]*dedup.ScheduleChange
	label                string
	selected             bool
}

func (c *YouTubeChecker) resolveYouTubeUpcomingSelection(
	ctx context.Context,
	stream *domain.Stream,
	subscriberRooms []string,
	window sharedchecker.EvaluationWindow,
) (youtubeUpcomingSelection, error) {
	targetPolicy := c.targetPolicySnapshot()
	currentMinutesUntil := sharedchecker.MinutesUntilFloorZeroClamped(*stream.StartScheduled, window.End)
	previousMinutesUntil := sharedchecker.MinutesUntilFloorZeroClamped(*stream.StartScheduled, window.Start)
	minutesUntil, targetCrossed := targetPolicy.HighestCrossed(*stream.StartScheduled, window)
	scheduleChanges, err := c.detectRoomScheduleChanges(ctx, stream, subscriberRooms)
	if err != nil {
		return youtubeUpcomingSelection{}, fmt.Errorf("build upcoming notifications: detect schedule change: %w", err)
	}
	if !targetCrossed && len(scheduleChanges) == 0 {
		observeYouTubeUpcomingNoMinuteDecision("no_target", window)
		return youtubeUpcomingSelection{}, nil
	}
	if !targetCrossed {
		minutesUntil = currentMinutesUntil
	}

	return youtubeUpcomingSelection{
		currentMinutesUntil:  currentMinutesUntil,
		previousMinutesUntil: previousMinutesUntil,
		minutesUntil:         minutesUntil,
		targetCrossed:        targetCrossed,
		scheduleChanges:      scheduleChanges,
		label:                youtubeUpcomingSelectionLabel(minutesUntil, currentMinutesUntil, targetCrossed),
		selected:             true,
	}, nil
}

func buildYouTubeUpcomingRoomNotifications(
	stream *domain.Stream,
	subscriberRooms []string,
	selection youtubeUpcomingSelection,
) []*domain.AlarmNotification {
	resolvedStream := ensureScheduledTime(stream, *stream.StartScheduled)
	notificationScheduleChanges := selection.scheduleChanges
	if selection.targetCrossed {
		notificationScheduleChanges = nil
	}
	return roomNotificationsWithScheduleChanges(
		subscriberRooms,
		resolvedStream.Channel,
		resolvedStream,
		selection.minutesUntil,
		notificationScheduleChanges,
		!selection.targetCrossed,
	)
}

func (c *YouTubeChecker) logYouTubeUpcomingSelection(
	stream *domain.Stream,
	selection youtubeUpcomingSelection,
	window sharedchecker.EvaluationWindow,
	roomCount int,
) {
	c.logger.Info("YouTube upcoming alarm selected",
		slog.String("stream_id", stream.ID),
		slog.String("channel_id", youtubeStreamChannelID(stream)),
		slog.Int("minutes_until", selection.minutesUntil),
		slog.Int("current_minutes_until", selection.currentMinutesUntil),
		slog.Int("previous_minutes_until", selection.previousMinutesUntil),
		slog.Bool("window_capped", window.Capped),
		slog.Bool("initial_observation", window.InitialObservation),
		slog.Time("window_start", window.Start),
		slog.Time("window_end", window.End),
		slog.Time("start_scheduled", stream.StartScheduled.UTC()),
		slog.String("selection", selection.label),
		slog.Int("rooms", roomCount),
	)
}

func (c *YouTubeChecker) detectRoomScheduleChanges(
	ctx context.Context,
	stream *domain.Stream,
	subscriberRooms []string,
) (map[string]*dedup.ScheduleChange, error) {
	if stream == nil {
		return nil, nil
	}

	channelID := stream.ChannelID
	if channelID == "" && stream.Channel != nil {
		channelID = stream.Channel.ID
	}

	changes := make(map[string]*dedup.ScheduleChange)
	for _, roomID := range uniqueStrings(subscriberRooms) {
		change, err := c.dedupSvc.DetectNotificationScheduleChange(ctx, roomID, channelID, stream)
		if err != nil {
			return nil, fmt.Errorf("detect room schedule changes: room %s: %w", roomID, err)
		}
		if change == nil {
			continue
		}
		changes[roomID] = change
	}

	return changes, nil
}

func (c *YouTubeChecker) targetMinutesSnapshot() []int {
	return c.targetPolicySnapshot().Clone()
}

func (c *YouTubeChecker) targetPolicySnapshot() sharedchecker.TargetMinutePolicy {
	c.targetMinutesMu.RLock()
	defer c.targetMinutesMu.RUnlock()

	return c.targetPolicy
}

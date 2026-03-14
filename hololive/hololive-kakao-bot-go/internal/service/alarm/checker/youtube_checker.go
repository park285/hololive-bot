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

package checker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/tier"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

const (
	channelProcessingConcurrency = 16
)

// YouTubeChecker는 Holodex live status 기반 알림 후보를 생성한다.
type YouTubeChecker struct {
	cacheSvc      cache.Client
	holodexSvc    *holodex.Service
	tierScheduler *tier.TieredScheduler
	dedupSvc      *dedup.Service
	targetMinutes []int
	logger        *slog.Logger
}

// NewYouTubeChecker는 YouTube 체커를 생성한다.
func NewYouTubeChecker(
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	tierScheduler *tier.TieredScheduler,
	dedupSvc *dedup.Service,
	targetMinutes []int,
	logger *slog.Logger,
) (*YouTubeChecker, error) {
	if cacheSvc == nil {
		return nil, fmt.Errorf("new youtube checker: cache service is nil")
	}
	if holodexSvc == nil {
		return nil, fmt.Errorf("new youtube checker: holodex service is nil")
	}
	if tierScheduler == nil {
		return nil, fmt.Errorf("new youtube checker: tier scheduler is nil")
	}
	if dedupSvc == nil {
		return nil, fmt.Errorf("new youtube checker: dedup service is nil")
	}

	return &YouTubeChecker{
		cacheSvc:      cacheSvc,
		holodexSvc:    holodexSvc,
		tierScheduler: tierScheduler,
		dedupSvc:      dedupSvc,
		targetMinutes: normalizeTargetMinutes(targetMinutes),
		logger:        safeLogger(logger),
	}, nil
}

// Check는 upcoming/live-catchup 알림 후보를 생성한다.
func (c *YouTubeChecker) Check(ctx context.Context) ([]*domain.AlarmNotification, error) {
	channelIDs, err := c.cacheSvc.SMembers(ctx, notification.AlarmChannelRegistryKey)
	if err != nil {
		return nil, fmt.Errorf("check youtube streams: read channel registry: %w", err)
	}
	if len(channelIDs) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	dueChannels := c.tierScheduler.SelectDueChannels(channelIDs)
	if len(dueChannels) == 0 {
		return []*domain.AlarmNotification{}, nil
	}

	streams, err := c.holodexSvc.GetChannelsLiveStatus(ctx, dueChannels)
	if err != nil {
		return nil, fmt.Errorf("check youtube streams: fetch channels live status: %w", err)
	}

	streamsByChannel := groupStreamsByChannel(streams)
	subscriberMap, err := loadSubscriberRoomsByChannel(ctx, c.cacheSvc, dueChannels)
	if err != nil {
		return nil, fmt.Errorf("check youtube streams: load subscriber rooms: %w", err)
	}

	now := time.Now().UTC()
	notifications := make([]*domain.AlarmNotification, 0, len(dueChannels)*5)
	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(channelProcessingConcurrency)

	for _, channelID := range dueChannels {
		channelStreams := streamsByChannel[channelID]
		if len(channelStreams) == 0 {
			channelStreams = []*domain.Stream{}
		}

		c.tierScheduler.UpdateChannelState(channelID, channelStreams)
		subscriberRooms := subscriberMap[channelID]
		if len(subscriberRooms) == 0 {
			continue
		}

		eg.Go(func() error {
			channelNotifications, channelErr := c.buildChannelNotifications(
				egCtx,
				channelID,
				subscriberRooms,
				channelStreams,
				now,
			)
			if channelErr != nil {
				return fmt.Errorf("check youtube streams: build channel notifications for %s: %w", channelID, channelErr)
			}

			if len(channelNotifications) == 0 {
				return nil
			}

			mu.Lock()
			notifications = append(notifications, channelNotifications...)
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("check youtube streams: wait channel workers: %w", err)
	}

	return notifications, nil
}

func (c *YouTubeChecker) buildChannelNotifications(
	ctx context.Context,
	channelID string,
	subscriberRooms []string,
	streams []*domain.Stream,
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(streams)*len(subscriberRooms))
	for _, stream := range streams {
		if stream == nil {
			continue
		}

		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, stream, subscriberRooms, now)
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build upcoming notifications: %w", err)
		}
		notifications = append(notifications, upcomingNotifications...)

		liveCatchupNotifications, err := c.buildLiveCatchupNotifications(ctx, channelID, stream, subscriberRooms, now)
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
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	if stream == nil || !stream.IsUpcoming() || stream.StartScheduled == nil {
		return nil, nil
	}
	if !stream.StartScheduled.After(now) {
		return nil, nil
	}

	minutesUntil := sharedchecker.MinutesUntilFloor(*stream.StartScheduled, now)
	if !sharedchecker.IsTargetMinute(c.targetMinutes, minutesUntil) {
		return nil, nil
	}

	alreadyNotified, err := c.dedupSvc.IsAlreadyNotifiedForSchedule(ctx, stream.ID, *stream.StartScheduled, minutesUntil)
	if err != nil {
		return nil, fmt.Errorf("build upcoming notifications: check already notified for schedule: %w", err)
	}
	if alreadyNotified {
		return nil, nil
	}

	resolvedStream := ensureScheduledTime(stream, *stream.StartScheduled)
	return roomNotifications(subscriberRooms, resolvedStream.Channel, resolvedStream, minutesUntil, ""), nil
}

func (c *YouTubeChecker) buildLiveCatchupNotifications(
	ctx context.Context,
	channelID string,
	stream *domain.Stream,
	subscriberRooms []string,
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	if stream == nil || !stream.IsLive() {
		return nil, nil
	}

	startAt := resolveLiveStart(stream)
	if startAt == nil || startAt.After(now) {
		return nil, nil
	}

	catchupWindow := sharedconstants.FullRefreshInterval + time.Minute
	if now.Sub(*startAt) > catchupWindow {
		return nil, nil
	}

	alreadyNotified, err := c.dedupSvc.IsAlreadyNotified(ctx, stream.ID)
	if err != nil {
		return nil, fmt.Errorf("build live catchup notifications: check already notified: %w", err)
	}
	if alreadyNotified {
		return nil, nil
	}

	resolvedStream := ensureScheduledTime(stream, *startAt)
	notifications := make([]*domain.AlarmNotification, 0, len(subscriberRooms))
	for _, roomID := range subscriberRooms {
		recentlyUpcoming, err := c.dedupSvc.WasUpcomingEventNotifiedRecently(
			ctx,
			roomID,
			channelID,
			resolvedStream,
			sharedconstants.LiveCatchupSuppressWindow,
		)
		if err != nil {
			return nil, fmt.Errorf("build live catchup notifications: check upcoming suppress window: %w", err)
		}
		if recentlyUpcoming {
			continue
		}
		notifications = append(notifications, roomNotifications([]string{roomID}, resolvedStream.Channel, resolvedStream, 0, "")...)
	}

	return notifications, nil
}

func resolveLiveStart(stream *domain.Stream) *time.Time {
	if stream == nil {
		return nil
	}
	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		start := stream.StartActual.UTC()
		return &start
	}
	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		start := stream.StartScheduled.UTC()
		return &start
	}
	return nil
}

func groupStreamsByChannel(streams []*domain.Stream) map[string][]*domain.Stream {
	grouped := make(map[string][]*domain.Stream)
	for _, stream := range streams {
		if stream == nil {
			continue
		}
		channelID := stream.ChannelID
		if channelID == "" && stream.Channel != nil {
			channelID = stream.Channel.ID
		}
		if channelID == "" {
			continue
		}

		grouped[channelID] = append(grouped[channelID], stream)
	}
	return grouped
}

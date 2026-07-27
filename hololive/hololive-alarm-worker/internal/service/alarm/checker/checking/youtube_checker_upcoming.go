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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
)

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

	alreadyNotified, err := c.dedupService.IsAlreadyNotifiedForSchedule(ctx, stream.ID, *stream.StartScheduled, selection.minutesUntil)
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

	var scheduleChanges map[string]*dedup.ScheduleChange
	if !targetCrossed {
		changes, err := c.detectRoomScheduleChanges(ctx, stream, subscriberRooms)
		if err != nil {
			return youtubeUpcomingSelection{}, fmt.Errorf("build upcoming notifications: detect schedule change: %w", err)
		}
		if len(changes) == 0 {
			observeYouTubeUpcomingNoMinuteDecision("no_target", window)
			return youtubeUpcomingSelection{}, nil
		}
		scheduleChanges = changes
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
	resolvedStream := EnsureScheduledTime(stream, *stream.StartScheduled)
	if resolvedStream == nil {
		return nil
	}
	notificationScheduleChanges := selection.scheduleChanges
	if selection.targetCrossed {
		notificationScheduleChanges = nil
	}
	return RoomNotificationsWithScheduleChanges(
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
	for _, roomID := range UniqueStrings(subscriberRooms) {
		change, err := c.dedupService.DetectNotificationScheduleChange(ctx, roomID, channelID, stream)
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

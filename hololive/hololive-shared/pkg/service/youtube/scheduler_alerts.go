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

package youtube

import (
	"context"
	"fmt"
	"log/slog"

	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/kapu/hololive-shared/pkg/util"
)

type milestoneAlertWork struct {
	notification ytstats.MilestoneNotification
	message      string
}

type approachingAlertWork struct {
	notification ytstats.ApproachingNotification
	message      string
}

func (ys *schedulerImpl) dispatchMilestoneAlerts(ctx context.Context) {
	if ys.alarmService == nil || ys.irisClient == nil {
		return
	}

	rooms, err := ys.alarmService.GetDistinctRooms(ctx)
	if err != nil {
		ys.logger.Warn("Failed to get alarm rooms for milestone dispatch", slog.Any("error", err))
		return
	}

	if len(rooms) == 0 {
		return
	}

	sendMessage := func(room, message string) error {
		return ys.irisClient.SendMessage(ctx, room, message)
	}

	if err := ys.SendMilestoneAlerts(ctx, sendMessage, rooms); err != nil {
		ys.logger.Warn("Failed to dispatch milestone alerts", slog.Any("error", err))
	}
}

func (ys *schedulerImpl) SendMilestoneAlerts(ctx context.Context, sendMessage func(room, message string) error, rooms []string) error {
	approachingSent := ys.sendApproachingAlerts(ctx, sendMessage, rooms)

	milestones, err := ys.statsRepo.GetUnnotifiedMilestones(ctx, 50)
	if err != nil {
		ys.logger.Warn("Failed to get unnotified milestones", slog.Any("error", err))
	}
	works := ys.buildMilestoneAlertWorks(ctx, milestones)
	sentNotifications := ys.dispatchMilestoneAlertWorks(ctx, sendMessage, rooms, works)
	ys.markMilestoneNotificationsSent(ctx, sentNotifications)

	milestoneSent := len(sentNotifications)
	totalSent := milestoneSent + approachingSent
	if totalSent > 0 {
		ys.logger.Info("Milestone notifications sent",
			slog.Int("achievements", milestoneSent),
			slog.Int("approaching", approachingSent))
	}

	return nil
}

func (ys *schedulerImpl) sendApproachingAlerts(ctx context.Context, sendMessage func(room, message string) error, rooms []string) int {
	notifications, err := ys.statsRepo.GetUnnotifiedApproaching(ctx, 50)
	if err != nil {
		ys.logger.Warn("Failed to get unnotified approaching alerts", slog.Any("error", err))
		return 0
	}

	if len(notifications) == 0 {
		return 0
	}
	works := ys.buildApproachingAlertWorks(ctx, notifications)
	sentNotifications := ys.dispatchApproachingAlertWorks(ctx, sendMessage, rooms, works)
	ys.markApproachingNotificationsSent(ctx, sentNotifications)
	return len(sentNotifications)
}

func (ys *schedulerImpl) buildMilestoneAlertWorks(
	ctx context.Context,
	milestones []ytstats.MilestoneNotification,
) []milestoneAlertWork {
	works := make([]milestoneAlertWork, 0, len(milestones))
	for _, milestone := range milestones {
		message, ok := ys.formatMilestoneAchievedMessage(ctx, milestone.MemberName, milestone.Value)
		if !ok {
			continue
		}
		works = append(works, milestoneAlertWork{
			notification: milestone,
			message:      message,
		})
	}

	return works
}

func (ys *schedulerImpl) formatMilestoneAchievedMessage(ctx context.Context, memberName string, value uint64) (string, bool) {
	formattedValue := util.FormatKoreanNumber(int64(value))
	if ys.formatter == nil {
		return fmt.Sprintf("🎉 %s님이 구독자 %s명을 달성했습니다!\n축하합니다! 🎊", memberName, formattedValue), true
	}

	message, err := ys.formatter.FormatMilestoneAchieved(ctx, memberName, formattedValue)
	if err != nil {
		ys.logger.Warn("마일스톤 달성 메시지 포맷 오류", slog.Any("error", err))
		return "", false
	}

	return message, true
}

func (ys *schedulerImpl) dispatchMilestoneAlertWorks(
	ctx context.Context,
	sendMessage func(room, message string) error,
	rooms []string,
	works []milestoneAlertWork,
) []ytstats.MilestoneNotification {
	return dispatchAlertWorks(
		ys.logger,
		ctx,
		sendMessage,
		rooms,
		works,
		func(work milestoneAlertWork) ytstats.MilestoneNotification { return work.notification },
		func(work milestoneAlertWork) string { return work.message },
		func(notification ytstats.MilestoneNotification) string { return notification.MemberName },
		"Failed to send milestone notification",
		"Milestone notification partially sent; keeping unsent state for retry",
	)
}

func (ys *schedulerImpl) markMilestoneNotificationsSent(ctx context.Context, notifications []ytstats.MilestoneNotification) {
	if len(notifications) == 0 {
		return
	}

	if err := ys.statsRepo.MarkMilestonesNotifiedBatch(ctx, notifications); err != nil {
		ys.logger.Warn("Failed to batch mark milestones notified",
			slog.Int("count", len(notifications)),
			slog.Any("error", err))
	}
}

func (ys *schedulerImpl) buildApproachingAlertWorks(
	ctx context.Context,
	notifications []ytstats.ApproachingNotification,
) []approachingAlertWork {
	works := make([]approachingAlertWork, 0, len(notifications))
	for _, notification := range notifications {
		works = append(works, approachingAlertWork{
			notification: notification,
			message:      ys.formatApproachingMessage(ctx, notification.MemberName, notification.MilestoneValue, notification.CurrentSubs),
		})
	}
	return works
}

func (ys *schedulerImpl) dispatchApproachingAlertWorks(
	ctx context.Context,
	sendMessage func(room, message string) error,
	rooms []string,
	works []approachingAlertWork,
) []ytstats.ApproachingNotification {
	return dispatchAlertWorks(
		ys.logger,
		ctx,
		sendMessage,
		rooms,
		works,
		func(work approachingAlertWork) ytstats.ApproachingNotification { return work.notification },
		func(work approachingAlertWork) string { return work.message },
		func(notification ytstats.ApproachingNotification) string { return notification.MemberName },
		"Failed to send approaching notification",
		"Approaching notification partially sent; keeping unsent state for retry",
	)
}

func (ys *schedulerImpl) markApproachingNotificationsSent(ctx context.Context, notifications []ytstats.ApproachingNotification) {
	if len(notifications) == 0 {
		return
	}

	if err := ys.statsRepo.MarkApproachingChatNotifiedBatch(ctx, notifications); err != nil {
		ys.logger.Warn("Failed to batch mark approaching notified",
			slog.Int("count", len(notifications)),
			slog.Any("error", err))
	}
}

func (ys *schedulerImpl) formatApproachingMessage(ctx context.Context, memberName string, milestone, currentSubs uint64) string {
	remaining := milestone - currentSubs
	if ys.formatter == nil {
		return fmt.Sprintf("📍 %s님이 구독자 %s명까지 %s명 남았습니다!\n곧 마일스톤 달성이 예상됩니다! 🎯",
			memberName,
			util.FormatKoreanNumber(int64(milestone)),
			util.FormatKoreanNumber(int64(remaining)))
	}

	msg, err := ys.formatter.FormatMilestoneApproaching(
		ctx,
		memberName,
		util.FormatKoreanNumber(int64(milestone)),
		util.FormatKoreanNumber(int64(remaining)),
	)
	if err != nil {
		return fmt.Sprintf("📍 %s님이 구독자 %s명까지 %s명 남았습니다!\n곧 마일스톤 달성이 예상됩니다! 🎯",
			memberName,
			util.FormatKoreanNumber(int64(milestone)),
			util.FormatKoreanNumber(int64(remaining)))
	}
	return msg
}

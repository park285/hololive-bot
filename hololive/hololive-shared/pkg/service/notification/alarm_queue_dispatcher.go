package notification

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
)

const (
	// dispatcherBackoff: BRPOP 에러 시 재시도 대기 시간
	dispatcherBackoff = 1 * time.Second
)

// AlarmQueueDispatcher: BRPOP 무한 루프로 큐 메시지를 소비하고 Iris로 발송하는 디스패처
type AlarmQueueDispatcher struct {
	consumer   *AlarmQueueConsumer
	alarm      domain.AlarmDispatchState
	irisClient iris.Client
	formatter  *adapter.ResponseFormatter
	logger     *slog.Logger
}

// NewAlarmQueueDispatcher: 큐 디스패처 생성
func NewAlarmQueueDispatcher(
	valkeyClient valkey.Client,
	alarm domain.AlarmDispatchState,
	irisClient iris.Client,
	formatter *adapter.ResponseFormatter,
	logger *slog.Logger,
) *AlarmQueueDispatcher {
	return &AlarmQueueDispatcher{
		consumer:   NewAlarmQueueConsumer(valkeyClient, logger),
		alarm:      alarm,
		irisClient: irisClient,
		formatter:  formatter,
		logger:     logger,
	}
}

// Run: 큐 소비 무한 루프 — context 취소 시 종료
func (d *AlarmQueueDispatcher) Run(ctx context.Context) {
	d.logger.Info("알림 큐 디스패처 시작")
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("알림 큐 디스패처 종료")
			return
		default:
		}

		envelopes, err := d.consumer.DrainBatch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			d.logger.Error("큐 소비 실패", slog.Any("error", err))
			time.Sleep(dispatcherBackoff)
			continue
		}

		if len(envelopes) == 0 {
			continue
		}

		d.processEnvelopes(ctx, envelopes)
	}
}

func (d *AlarmQueueDispatcher) processEnvelopes(ctx context.Context, envelopes []*domain.AlarmQueueEnvelope) {
	// envelope에서 AlarmNotification 추출
	notifications := make([]*domain.AlarmNotification, 0, len(envelopes))
	for _, env := range envelopes {
		n := env.Notification
		notifications = append(notifications, &n)
	}

	// 기존 그룹화 로직 재사용
	groups := groupQueueNotifications(notifications)

	for _, group := range groups {
		if group == nil || len(group.notifications) == 0 {
			continue
		}

		// claim은 Rust가 이미 수행 — 바로 렌더링 + 발송
		message := d.renderGroupMessage(ctx, group)
		if message == "" {
			// 렌더링 실패 → claim 해제
			failedKeys := collectClaimKeys(envelopes, group)
			d.consumer.ReleaseClaimKeys(ctx, failedKeys)
			continue
		}

		if err := d.irisClient.SendMessage(ctx, group.roomID, message); err != nil {
			d.logger.Error("큐 알림 발송 실패",
				slog.String("room", group.roomID),
				slog.Int("notifications", len(group.notifications)),
				slog.Any("error", err),
			)
			// 발송 실패 → claim 해제 (재시도 허용)
			failedKeys := collectClaimKeys(envelopes, group)
			d.consumer.ReleaseClaimKeys(ctx, failedKeys)
			continue
		}

		d.logger.Info("큐 알림 발송 성공",
			slog.String("room", group.roomID),
			slog.Int("notifications", len(group.notifications)),
		)

		// 발송 성공 → mark_as_notified + mark_upcoming_event_notified
		d.markGroupNotified(ctx, group)
	}
}

func (d *AlarmQueueDispatcher) renderGroupMessage(ctx context.Context, group *queueNotificationGroup) string {
	if len(group.notifications) == 1 {
		return d.formatter.AlarmNotification(ctx, group.notifications[0])
	}
	return d.formatter.AlarmNotificationGroup(group.minutesUntil, group.notifications)
}

func (d *AlarmQueueDispatcher) markGroupNotified(ctx context.Context, group *queueNotificationGroup) {
	for _, notif := range group.notifications {
		if notif == nil || notif.Stream == nil || notif.Stream.StartScheduled == nil {
			continue
		}

		if err := d.alarm.MarkAsNotified(ctx, notif.Stream.ID, *notif.Stream.StartScheduled, notif.MinutesUntil); err != nil {
			d.logger.Warn("mark_as_notified 실패",
				slog.String("stream_id", notif.Stream.ID),
				slog.Any("error", err),
			)
		}

		if notif.MinutesUntil > 0 {
			channelID := resolveQueueNotificationChannelID(notif)
			if err := d.alarm.MarkUpcomingEventNotified(ctx, group.roomID, channelID, notif.Stream); err != nil {
				d.logger.Warn("mark_upcoming_event_notified 실패",
					slog.String("stream_id", notif.Stream.ID),
					slog.Any("error", err),
				)
			}
		}
	}
}

// collectClaimKeys: 그룹에 속한 notification들의 claim 키를 envelope에서 수집
func collectClaimKeys(envelopes []*domain.AlarmQueueEnvelope, group *queueNotificationGroup) []string {
	groupNotifSet := make(map[string]struct{})
	for _, notif := range group.notifications {
		if notif != nil && notif.Stream != nil {
			groupNotifSet[notif.RoomID+"|"+notif.Stream.ID] = struct{}{}
		}
	}

	keys := make([]string, 0)
	for _, env := range envelopes {
		key := env.Notification.RoomID
		if env.Notification.Stream != nil {
			key += "|" + env.Notification.Stream.ID
		}
		if _, ok := groupNotifSet[key]; ok {
			keys = append(keys, env.ClaimKeys...)
		}
	}
	return keys
}

// resolveQueueNotificationChannelID: AlarmNotification에서 채널 ID를 추출
func resolveQueueNotificationChannelID(notif *domain.AlarmNotification) string {
	if notif == nil {
		return ""
	}
	if notif.Channel != nil && notif.Channel.ID != "" {
		return notif.Channel.ID
	}
	if notif.Stream != nil {
		return notif.Stream.ChannelID
	}
	return ""
}

// queueNotificationGroup: 동일 방/시간 기준 그룹 (notification 패키지 내 큐 전용)
type queueNotificationGroup struct {
	roomID        string
	minutesUntil  int
	notifications []*domain.AlarmNotification
}

// groupQueueNotifications: 방별/시간별 알림 그룹화
func groupQueueNotifications(notifications []*domain.AlarmNotification) []*queueNotificationGroup {
	if len(notifications) == 0 {
		return nil
	}

	groups := make([]*queueNotificationGroup, 0)
	index := make(map[string]int)

	for _, notif := range notifications {
		if notif == nil {
			continue
		}

		key := buildQueueGroupKey(notif)
		if idx, ok := index[key]; ok {
			group := groups[idx]
			group.notifications = append(group.notifications, notif)
			if notif.MinutesUntil >= 0 && (group.minutesUntil < 0 || notif.MinutesUntil < group.minutesUntil) {
				group.minutesUntil = notif.MinutesUntil
			}
			continue
		}

		group := &queueNotificationGroup{
			roomID:        notif.RoomID,
			minutesUntil:  notif.MinutesUntil,
			notifications: []*domain.AlarmNotification{notif},
		}
		groups = append(groups, group)
		index[key] = len(groups) - 1
	}

	return groups
}

func buildQueueGroupKey(notif *domain.AlarmNotification) string {
	if notif == nil {
		return ""
	}
	if notif.Stream != nil && notif.Stream.StartScheduled != nil {
		scheduled := notif.Stream.StartScheduled.Truncate(time.Minute)
		return fmt.Sprintf("%s|scheduled|%d", notif.RoomID, scheduled.Unix())
	}
	return fmt.Sprintf("%s|minutes|%d", notif.RoomID, notif.MinutesUntil)
}

package workerapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func renderAlarmDispatchGroup(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, group alarmDispatchGroup) (string, error) {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return renderCelebrationMessage(ctx, renderer, &group.envelopes[0])
	}
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return renderAlarmDispatchYouTubeOutbox(ctx, renderer, messageStrings, &group.envelopes[0])
	}
	if len(group.notifications) == 1 {
		return renderAlarmDispatchNotification(ctx, renderer, messageStrings, &group.notifications[0])
	}
	return renderAlarmDispatchNotificationGroup(ctx, renderer, messageStrings, group)
}

func renderAlarmDispatchYouTubeOutbox(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.YouTubeOutbox == nil {
		return "", fmt.Errorf("render youtube outbox dispatch: payload is nil")
	}
	return outbox.FormatYouTubeOutboxPayload(ctx, renderer, messageStrings, envelope.YouTubeOutbox)
}

type alarmDispatchItemView struct {
	MemberName      string
	Title           string
	URL             string
	ScheduleMessage string
	MinutesUntil    int
	IsStarting      bool
	IsScheduled     bool
}

type alarmDispatchGroupView struct {
	MinutesUntil int
	IsStarting   bool
	Entries      []alarmDispatchItemView
}

func buildAlarmDispatchItemView(ctx context.Context, store *messagestrings.Store, notification *domain.AlarmNotification, groupMinutesUntil int) alarmDispatchItemView {
	starting := alarmDispatchNotificationIsStarting(notification)
	return alarmDispatchItemView{
		MemberName:      resolveAlarmDispatchMemberName(ctx, store, notification),
		Title:           resolveAlarmDispatchTitle(ctx, store, notification),
		URL:             resolveAlarmDispatchURL(notification),
		ScheduleMessage: strings.TrimSpace(notification.ScheduleChangeMessage),
		MinutesUntil:    notification.MinutesUntil,
		IsStarting:      starting,
		IsScheduled:     !starting && groupMinutesUntil > 0 && notification.MinutesUntil == groupMinutesUntil,
	}
}

func alarmDispatchNotificationIsStarting(notification *domain.AlarmNotification) bool {
	if notification == nil {
		return false
	}
	if notification.MinutesUntil <= 0 {
		return true
	}
	if notification.Stream == nil {
		return false
	}
	return notification.Stream.IsLive() || notification.Stream.StartActual != nil
}

func alarmDispatchGroupAllStarting(group alarmDispatchGroup) bool {
	if len(group.notifications) == 0 {
		return group.minutesUntil <= 0
	}
	for i := range group.notifications {
		if !alarmDispatchNotificationIsStarting(&group.notifications[i]) {
			return false
		}
	}
	return true
}

func renderAlarmDispatchNotificationGroup(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, group alarmDispatchGroup) (string, error) {
	entries := make([]alarmDispatchItemView, 0, len(group.notifications))
	for i := range group.notifications {
		entries = append(entries, buildAlarmDispatchItemView(ctx, store, &group.notifications[i], group.minutesUntil))
	}
	view := alarmDispatchGroupView{
		MinutesUntil: group.minutesUntil,
		IsStarting:   alarmDispatchGroupAllStarting(group),
		Entries:      entries,
	}
	message, err := renderer.Render(ctx, domain.TemplateKeyAlarmDispatchNotificationGroup, "", view)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: %w", err)
	}
	return message, nil
}

func renderAlarmDispatchNotification(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, notification *domain.AlarmNotification) (string, error) {
	view := buildAlarmDispatchItemView(ctx, store, notification, -1)
	message, err := renderer.Render(ctx, domain.TemplateKeyAlarmDispatchNotification, "", view)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification: %w", err)
	}
	return message, nil
}

func resolveAlarmDispatchMemberName(ctx context.Context, store *messagestrings.Store, notification *domain.AlarmNotification) string {
	if notification.Channel != nil && strings.TrimSpace(notification.Channel.Name) != "" {
		return strings.TrimSpace(notification.Channel.Name)
	}
	if notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelName) != "" {
		return strings.TrimSpace(notification.Stream.ChannelName)
	}
	return alarmDispatchMessageString(ctx, store, "alarm_unknown_member", "알 수 없는 멤버")
}

func resolveAlarmDispatchTitle(ctx context.Context, store *messagestrings.Store, notification *domain.AlarmNotification) string {
	if notification.Stream == nil {
		return alarmDispatchMessageString(ctx, store, "alarm_no_stream", "방송 정보 없음")
	}
	if title := strings.TrimSpace(notification.Stream.Title); title != "" {
		return title
	}
	return alarmDispatchMessageString(ctx, store, "alarm_no_title", "제목 없음")
}

func alarmDispatchMessageString(ctx context.Context, store *messagestrings.Store, key, fallback string) string {
	if value := store.GetContext(ctx, messagestrings.NamespaceMisc, key); value != "" {
		return value
	}
	return fallback
}

func resolveAlarmDispatchURL(notification *domain.AlarmNotification) string {
	if notification.Stream == nil {
		return ""
	}
	stream := notification.Stream
	if url, ok := resolveAlarmDispatchDirectPlatformURL(stream); ok {
		return url
	}
	if stream.IsIntegrated {
		return resolveAlarmDispatchIntegratedURL(stream)
	}
	return stream.GetYouTubeURL()
}

func resolveAlarmDispatchDirectPlatformURL(stream *domain.Stream) (string, bool) {
	if stream.IsTwitchOnly && stream.GetTwitchLiveURL() != "" {
		return stream.GetTwitchLiveURL(), true
	}
	if stream.IsChzzkOnly && stream.GetChzzkLiveURL() != "" {
		return stream.GetChzzkLiveURL(), true
	}
	return "", false
}

func resolveAlarmDispatchIntegratedURL(stream *domain.Stream) string {
	youtubeURL := stream.GetYouTubeURL()
	if youtubeURL == "" {
		return ""
	}
	if chzzkURL := stream.GetChzzkLiveURL(); chzzkURL != "" {
		return fmt.Sprintf("%s | %s", youtubeURL, chzzkURL)
	}
	return youtubeURL
}

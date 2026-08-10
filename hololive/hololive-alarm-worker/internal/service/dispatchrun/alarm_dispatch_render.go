package dispatchrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatch"
)

func renderAlarmDispatchGroup(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, group alarmDispatchGroup) (string, error) {
	if message, handled, err := renderAlarmDispatchGroupSource(ctx, renderer, messageStrings, group); handled {
		return message, err
	}
	if len(group.notifications) == 1 {
		return renderAlarmDispatchNotification(ctx, renderer, messageStrings, &group.notifications[0])
	}
	return renderAlarmDispatchNotificationGroup(ctx, renderer, messageStrings, group)
}

func renderAlarmDispatchGroupSource(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, group alarmDispatchGroup) (message string, handled bool, err error) {
	if len(group.envelopes) == 0 {
		return "", false, nil
	}
	envelope := &group.envelopes[0]
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		message, err = renderCelebrationMessage(ctx, renderer, envelope)
		return message, true, err
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		message, err = renderAlarmDispatchYouTubeOutbox(ctx, renderer, messageStrings, envelope)
		return message, true, err
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest {
		if envelope.DeliveryDigest == nil {
			return "", true, fmt.Errorf("render delivery digest dispatch: payload is nil")
		}
		return envelope.DeliveryDigest.PreRenderedMessage, true, nil
	}
	return "", false, nil
}

func renderAlarmDispatchYouTubeOutbox(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.YouTubeOutbox == nil {
		return "", fmt.Errorf("render youtube outbox dispatch: payload is nil")
	}
	return dispatch.FormatYouTubeOutboxPayload(ctx, renderer, messageStrings, envelope.YouTubeOutbox)
}

type alarmDispatchItemView struct {
	MemberName      string
	Title           string
	URL             string
	ScheduleMessage string
	MinutesUntil    int
	IsStarting      bool
	IsScheduled     bool
	IsPremiere      bool
}

type alarmDispatchGroupView struct {
	MinutesUntil int
	IsStarting   bool
	AllPremiere  bool
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
		IsPremiere:      notification.Stream != nil && notification.Stream.IsPremiere,
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

func alarmDispatchGroupAllPremiere(group alarmDispatchGroup) bool {
	if len(group.notifications) == 0 {
		return false
	}
	for i := range group.notifications {
		if group.notifications[i].Stream == nil || !group.notifications[i].Stream.IsPremiere {
			return false
		}
	}
	return true
}

func buildAlarmDispatchGroupView(ctx context.Context, store *messagestrings.Store, group alarmDispatchGroup) alarmDispatchGroupView {
	return buildAlarmDispatchGroupViewWithShortLinks(ctx, store, group, shortlinkservice.YouTubeBuilder{})
}

func buildAlarmDispatchGroupViewWithShortLinks(
	ctx context.Context,
	store *messagestrings.Store,
	group alarmDispatchGroup,
	shortLinks shortlinkservice.YouTubeBuilder,
) alarmDispatchGroupView {
	entries := make([]alarmDispatchItemView, 0, len(group.notifications))
	for i := range group.notifications {
		entry := buildAlarmDispatchItemView(ctx, store, &group.notifications[i], group.minutesUntil)
		entry.URL = resolveAlarmDispatchGroupURL(&group.notifications[i], shortLinks)
		entries = append(entries, entry)
	}
	return alarmDispatchGroupView{
		MinutesUntil: group.minutesUntil,
		IsStarting:   alarmDispatchGroupAllStarting(group),
		AllPremiere:  alarmDispatchGroupAllPremiere(group),
		Entries:      entries,
	}
}

func renderAlarmDispatchNotificationGroup(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, group alarmDispatchGroup) (string, error) {
	shortLinks, err := configuredAlarmShortLinkBuilder()
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: short links: %w", err)
	}
	message, err := renderer.Render(
		ctx,
		domain.TemplateKeyAlarmDispatchNotificationGroup,
		"",
		buildAlarmDispatchGroupViewWithShortLinks(ctx, store, group, shortLinks),
	)
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
	if notification == nil || notification.Stream == nil {
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

func resolveAlarmDispatchGroupURL(notification *domain.AlarmNotification, shortLinks shortlinkservice.YouTubeBuilder) string {
	if notification == nil {
		return ""
	}
	resolved := resolveAlarmDispatchURL(notification)
	if notification.Stream == nil || !shortLinks.Enabled() {
		return resolved
	}

	stream := notification.Stream
	if _, direct := resolveAlarmDispatchDirectPlatformURL(stream); direct {
		return resolved
	}
	shortURL, ok := shortLinks.URL(stream.ID)
	if !ok {
		return resolved
	}
	if stream.IsIntegrated {
		if chzzkURL := stream.GetChzzkLiveURL(); chzzkURL != "" {
			return fmt.Sprintf("%s | %s", shortURL, chzzkURL)
		}
	}
	return shortURL
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

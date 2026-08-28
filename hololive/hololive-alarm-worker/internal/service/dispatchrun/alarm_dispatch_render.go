package dispatchrun

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func renderAlarmDispatchGroup(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) (string, error) {
	if message, handled, err := renderAlarmDispatchGroupSource(ctx, renderer, messageStrings, group); handled {
		if err != nil {
			return message, fmt.Errorf("render alarm dispatch group source: %w", err)
		}

		return message, nil
	}

	if len(group.notifications) == 1 {
		out, err := renderAlarmDispatchNotification(ctx, renderer, messageStrings, members, &group.notifications[0])
		if err != nil {
			return out, fmt.Errorf("render alarm dispatch notification: %w", err)
		}

		return out, nil
	}

	out, err := renderAlarmDispatchNotificationGroup(ctx, renderer, messageStrings, members, group)
	if err != nil {
		return out, fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	return out, nil
}

func renderAlarmDispatchGroupSource(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, group alarmDispatchGroup) (message string, handled bool, err error) {
	if len(group.envelopes) == 0 {
		return "", false, nil
	}

	envelope := &group.envelopes[0]

	type sourceRenderer struct {
		action string
		run    func() (string, error)
	}

	renderers := map[domain.AlarmDispatchSourceKind]sourceRenderer{
		domain.AlarmDispatchSourceKindCelebration: {
			action: "render celebration message",
			run:    func() (string, error) { return renderCelebrationMessage(ctx, renderer, envelope) },
		},
		domain.AlarmDispatchSourceKindYouTubeOutbox: {
			action: "render alarm dispatch youtube outbox",
			run: func() (string, error) {
				return renderAlarmDispatchYouTubeOutbox(ctx, renderer, messageStrings, envelope)
			},
		},
		domain.AlarmDispatchSourceKindDeliveryDigest: {
			action: "render delivery digest dispatch",
			run:    func() (string, error) { return renderAlarmDispatchDeliveryDigest(envelope) },
		},
	}

	selected, ok := renderers[envelope.SourceKind]
	if !ok {
		return "", false, nil
	}

	message, err = selected.run()
	if err != nil {
		return message, true, fmt.Errorf("%s: %w", selected.action, err)
	}

	return message, true, nil
}

func renderAlarmDispatchDeliveryDigest(envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.DeliveryDigest == nil {
		return "", errors.New("payload is nil")
	}

	return envelope.DeliveryDigest.PreRenderedMessage, nil
}

func renderAlarmDispatchYouTubeOutbox(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, envelope *domain.AlarmQueueEnvelope) (string, error) {
	if envelope.YouTubeOutbox == nil {
		return "", errors.New("render youtube outbox dispatch: payload is nil")
	}

	out, err := youtubedispatch.FormatYouTubeOutboxPayload(ctx, renderer, messageStrings, envelope.YouTubeOutbox)
	if err != nil {
		return out, fmt.Errorf("format youtube outbox payload: %w", err)
	}

	return out, nil
}

type alarmDispatchItemView struct {
	MemberName      string
	Title           string
	URL             string
	CollabMembers   string
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

func buildAlarmDispatchItemView(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, notification *domain.AlarmNotification, groupMinutesUntil int) alarmDispatchItemView {
	starting := notification.IsStarting()

	return alarmDispatchItemView{
		MemberName:      resolveAlarmDispatchMemberName(ctx, store, notification),
		Title:           resolveAlarmDispatchTitle(ctx, store, notification),
		URL:             resolveAlarmDispatchURL(notification),
		CollabMembers:   formatAlarmDispatchCollabMembers(members, notification.Stream),
		ScheduleMessage: strings.TrimSpace(notification.ScheduleChangeMessage),
		MinutesUntil:    notification.MinutesUntil,
		IsStarting:      starting,
		IsScheduled:     !starting && groupMinutesUntil > 0 && notification.MinutesUntil == groupMinutesUntil,
		IsPremiere:      notification.Stream != nil && notification.Stream.IsPremiere,
	}
}

func formatAlarmDispatchCollabMembers(members domain.MemberDataProvider, stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	return officialidentity.Format(officialidentity.DisplayNames(members, stream.CollaboTalentNames, stream.ChannelID))
}

func alarmDispatchGroupAllStarting(group alarmDispatchGroup) bool {
	if len(group.notifications) == 0 {
		return group.minutesUntil <= 0
	}

	for i := range group.notifications {
		if !group.notifications[i].IsStarting() {
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

func buildAlarmDispatchGroupView(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) alarmDispatchGroupView {
	return buildAlarmDispatchGroupViewWithShortLinks(ctx, store, members, group, shortlinkservice.YouTubeBuilder{})
}

func buildAlarmDispatchGroupViewWithShortLinks(
	ctx context.Context,
	store *messagestrings.Store,
	members domain.MemberDataProvider,
	group alarmDispatchGroup,
	shortLinks shortlinkservice.YouTubeBuilder,
) alarmDispatchGroupView {
	entries := make([]alarmDispatchItemView, 0, len(group.notifications))
	for i := range group.notifications {
		entry := buildAlarmDispatchItemView(ctx, store, members, &group.notifications[i], group.minutesUntil)

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

func renderAlarmDispatchNotificationGroup(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) (string, error) {
	shortLinks, err := configuredAlarmShortLinkBuilder()
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: short links: %w", err)
	}

	message, err := renderer.Render(
		ctx,
		domain.TemplateKeyAlarmDispatchNotificationGroup,
		"",
		buildAlarmDispatchGroupViewWithShortLinks(ctx, store, members, group, shortLinks),
	)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	return message, nil
}

func renderAlarmDispatchNotification(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members domain.MemberDataProvider, notification *domain.AlarmNotification) (string, error) {
	view := buildAlarmDispatchItemView(ctx, store, members, notification, -1)

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

func resolveAlarmDispatchKaringURL(notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	stream := notification.Stream
	if url, ok := resolveAlarmDispatchDirectPlatformURL(stream); ok {
		return url
	}

	if youtubeURL := stream.GetYouTubeURL(); youtubeURL != "" {
		return youtubeURL
	}

	return stream.GetChzzkLiveURL()
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

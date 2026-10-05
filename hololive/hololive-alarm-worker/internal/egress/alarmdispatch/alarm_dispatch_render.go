package alarmdispatch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/format"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// renderAlarmDispatchGroup는 알림을 자동 접기 없이 렌더링하고 사전 렌더된 다이제스트는 그대로 유지한다.
func renderAlarmDispatchGroup(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, members domain.MemberDataProvider, shortLinkBaseURL string, group alarmDispatchGroup) (string, error) {
	if message, handled, err := renderAlarmDispatchGroupSource(ctx, renderer, messageStrings, members, group); handled {
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

	out, err := renderAlarmDispatchNotificationGroup(ctx, renderer, messageStrings, members, shortLinkBaseURL, group)
	if err != nil {
		return out, fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	return out, nil
}

func renderAlarmDispatchGroupSource(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) (message string, handled bool, err error) {
	if len(group.envelopes) == 0 {
		return "", false, nil
	}

	envelope := &group.envelopes[0]

	type sourceRenderer struct {
		action string
		run    func() (string, error)
	}

	renderers := map[domain.AlarmDispatchSourceKind]sourceRenderer{
		domain.AlarmDispatchSourceKindXSpace: {
			action: "render x space start",
			run: func() (string, error) {
				if validationErr := envelope.ValidateCanonicalDispatch(); validationErr != nil {
					return "", fmt.Errorf("validate x space: %w", validationErr)
				}

				memberName, nameErr := alarmContractMemberName(ctx, messageStrings, members, envelope.XSpace.ChannelID)
				if nameErr != nil {
					return "", fmt.Errorf("resolve x space member name: %w", nameErr)
				}

				return renderer.Render(ctx, domain.TemplateKeyXSpaceStarted, "", xSpaceStartView{
					MemberName: memberName, Title: envelope.XSpace.Title, URL: envelope.XSpace.URL(),
				})
			},
		},
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

	out, err := format.FormatYouTubeOutboxPayload(ctx, renderer, messageStrings, envelope.YouTubeOutbox)
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

func buildAlarmDispatchItemView(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, notification *domain.AlarmNotification, groupMinutesUntil int) (alarmDispatchItemView, error) {
	starting := notification.IsStarting()

	collabMembers, err := formatAlarmDispatchCollabMembers(ctx, members, notification.Stream)
	if err != nil {
		return alarmDispatchItemView{}, err
	}

	memberName, err := resolveAlarmDispatchMemberName(ctx, store, members, notification)
	if err != nil {
		return alarmDispatchItemView{}, err
	}

	return alarmDispatchItemView{
		MemberName:      memberName,
		Title:           resolveAlarmDispatchTitle(ctx, store, notification),
		URL:             resolveAlarmDispatchURL(notification),
		CollabMembers:   collabMembers,
		ScheduleMessage: strings.TrimSpace(notification.ScheduleChangeMessage),
		MinutesUntil:    notification.MinutesUntil,
		IsStarting:      starting,
		IsScheduled:     !starting && groupMinutesUntil > 0 && notification.MinutesUntil == groupMinutesUntil,
		IsPremiere:      notification.Stream != nil && notification.Stream.IsPremiere,
	}, nil
}

// 콜라보 표시명에 필요한 멤버를 적재하지 못하면 이름을 빼고 보내지 않고 렌더 실패로 돌려준다(발송 전 실패로 재시도).
func formatAlarmDispatchCollabMembers(ctx context.Context, members domain.MemberDataProvider, stream *domain.Stream) (string, error) {
	if stream == nil {
		return "", nil
	}

	names, err := officialidentity.DisplayNames(ctx, members, stream.CollaboTalentNames, stream.ChannelID)
	if err != nil {
		return "", fmt.Errorf("format collab members: %w", err)
	}

	return officialidentity.Format(names), nil
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

func buildAlarmDispatchGroupView(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) (alarmDispatchGroupView, error) {
	return buildAlarmDispatchGroupViewWithShortLinks(ctx, store, members, group, shortlinkservice.YouTubeBuilder{})
}

func buildAlarmDispatchGroupViewWithShortLinks(
	ctx context.Context,
	store *messagestrings.Store,
	members domain.MemberDataProvider,
	group alarmDispatchGroup,
	shortLinks shortlinkservice.YouTubeBuilder,
) (alarmDispatchGroupView, error) {
	entries := make([]alarmDispatchItemView, 0, len(group.notifications))
	for i := range group.notifications {
		entry, err := buildAlarmDispatchItemView(ctx, store, members, &group.notifications[i], group.minutesUntil)
		if err != nil {
			return alarmDispatchGroupView{}, err
		}

		entry.URL = resolveAlarmDispatchGroupURL(&group.notifications[i], shortLinks)
		entries = append(entries, entry)
	}

	return alarmDispatchGroupView{
		MinutesUntil: group.minutesUntil,
		IsStarting:   alarmDispatchGroupAllStarting(group),
		AllPremiere:  alarmDispatchGroupAllPremiere(group),
		Entries:      entries,
	}, nil
}

func renderAlarmDispatchNotificationGroup(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members domain.MemberDataProvider, shortLinkBaseURL string, group alarmDispatchGroup) (string, error) {
	shortLinks, err := configuredAlarmShortLinkBuilder(shortLinkBaseURL)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: short links: %w", err)
	}

	view, err := buildAlarmDispatchGroupViewWithShortLinks(ctx, store, members, group, shortLinks)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	message, err := renderer.Render(
		ctx,
		domain.TemplateKeyAlarmDispatchNotificationGroup,
		"",
		view,
	)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	return message, nil
}

func renderAlarmDispatchNotification(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members domain.MemberDataProvider, notification *domain.AlarmNotification) (string, error) {
	view, err := buildAlarmDispatchItemView(ctx, store, members, notification, -1)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification: %w", err)
	}

	message, err := renderer.Render(ctx, domain.TemplateKeyAlarmDispatchNotification, "", view)
	if err != nil {
		return "", fmt.Errorf("render alarm dispatch notification: %w", err)
	}

	return message, nil
}

// xSpaceStartView는 X_SPACE_STARTED 템플릿 자료다. 멤버 이름은 저장된 payload가 아니라 렌더 때 members에서 정한다.
type xSpaceStartView struct {
	MemberName string
	Title      string
	URL        string
}

// resolveAlarmDispatchMemberName은 방송 알림의 멤버 이름에 호스트 표기를 붙인다. 원천 채널 제목은 쓰지 않는다.
func resolveAlarmDispatchMemberName(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, notification *domain.AlarmNotification) (string, error) {
	channelID := alarmNotificationChannelID(notification)

	name, err := alarmContractMemberName(ctx, store, members, channelID)
	if err != nil {
		return "", fmt.Errorf("resolve alarm member name: %w", err)
	}

	if notification.Stream == nil {
		return name, nil
	}

	return mekparkhost.DisplayName(channelID, notification.Stream.Title, name), nil
}

func alarmNotificationChannelID(notification *domain.AlarmNotification) string {
	if notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelID) != "" {
		return strings.TrimSpace(notification.Stream.ChannelID)
	}

	if notification.Channel != nil {
		return strings.TrimSpace(notification.Channel.ID)
	}

	return ""
}

// alarmContractMemberName은 멤버 표시명 예외 계약(members short_korean_name→korean_name→misc/vtuber_fallback)을 따른다.
// members 조회 오류는 다른 이름으로 덮지 않고 렌더 실패(발송 전 재시도)로 돌려준다.
func alarmContractMemberName(ctx context.Context, store *messagestrings.Store, members domain.MemberDataProvider, channelID string) (string, error) {
	name, err := alarmDispatchMemberDisplayName(ctx, members, channelID)
	if err != nil {
		return "", err
	}

	if name != "" {
		return name, nil
	}

	alarmDispatchMemberNameMissingTotal().Inc()

	return store.Text(messagestrings.MiscVTuberFallback), nil
}

// alarmDispatchMemberDisplayName은 members에 행이 없거나 한국어 표시명이 모두 비면 빈 문자열을 돌려준다.
func alarmDispatchMemberDisplayName(ctx context.Context, members domain.MemberDataProvider, channelID string) (string, error) {
	if members == nil {
		return "", errors.New("member data provider is not configured")
	}

	if channelID == "" {
		return "", nil
	}

	member, err := members.FindMemberByChannelID(ctx, channelID)
	if errors.Is(err, domain.ErrMemberNotFound) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("find member %q: %w", channelID, err)
	}

	return cmp.Or(strings.TrimSpace(member.ShortKoreanName), strings.TrimSpace(member.NameKo)), nil
}

func resolveAlarmDispatchTitle(_ context.Context, store *messagestrings.Store, notification *domain.AlarmNotification) string {
	if notification.Stream == nil {
		return store.Text(messagestrings.MiscAlarmNoStream)
	}

	if title := strings.TrimSpace(notification.Stream.Title); title != "" {
		return title
	}

	return store.Text(messagestrings.MiscAlarmNoTitle)
}

func resolveAlarmDispatchURL(notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	stream := notification.Stream
	if !stream.HasYouTubeInfo() {
		return ""
	}

	return stream.GetYouTubeURL()
}

func resolveAlarmDispatchGroupURL(notification *domain.AlarmNotification, shortLinks shortlinkservice.YouTubeBuilder) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	resolved := resolveAlarmDispatchURL(notification)
	if resolved == "" || !shortLinks.Enabled() {
		return resolved
	}

	if shortURL, ok := shortLinks.URL(notification.Stream.ID); ok {
		return shortURL
	}

	return resolved
}

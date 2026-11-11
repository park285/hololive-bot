package alarmdispatch

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/officialidentity"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

// renderTemplateVariants는 같은 자료로 두 본문 변형을 렌더하고 두 변형의 template 버전을 한 문장으로 확인한다. 어느 변형을
// 보낼지는 발송기가 방 유형으로 한 번 정한다.
func renderTemplateVariants(ctx context.Context, renderer *template.Renderer, key domain.TemplateKey, data any) (delivery.MessageVariants, error) {
	results, err := renderer.RenderBatch(ctx, []template.RenderRequest{
		{Key: key, Format: domain.TemplateFormatPlain, Data: data},
		{Key: key, Format: domain.TemplateFormatRichText, Data: data},
	})
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render %s: %w", key, err)
	}

	if err := results[0].Err; err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render plain %s: %w", key, err)
	}

	if err := results[1].Err; err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render richtext %s: %w", key, err)
	}

	return delivery.MessageVariants{Plain: results[0].Text, RichText: results[1].Text}, nil
}

// alarmDispatchMembers는 한 claim 배치의 렌더가 함께 쓰는 멤버 조회다. 콜라보 공식 표기명 색인은 콜라보 이름이 처음 나올 때
// 만들어 같은 배치의 group이 다시 쓰고 배치와 함께 버리므로, 멤버 변경은 다음 배치부터 반영된다. 색인 적재 실패는 남기지 않는다.
// Runner가 group을 차례로 렌더하므로 동시에 쓰지 않는다.
type alarmDispatchMembers struct {
	provider domain.MemberDataProvider
	index    officialidentity.Index
}

func (m *alarmDispatchMembers) collabIndex(ctx context.Context) (officialidentity.Index, error) {
	if m.index != nil {
		return m.index, nil
	}

	index, err := officialidentity.Build(ctx, m.provider)
	if err != nil {
		return nil, fmt.Errorf("collab index: %w", err)
	}

	m.index = index

	return index, nil
}

// renderAlarmDispatchGroup는 알림을 자동 접기 없이 렌더링하고 사전 렌더된 다이제스트는 그대로 유지한다.
func renderAlarmDispatchGroup(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, members *alarmDispatchMembers, shortLinkBaseURL string, group alarmDispatchGroup) (delivery.MessageVariants, error) {
	if message, handled, err := renderAlarmDispatchGroupSource(ctx, renderer, messageStrings, members.provider, group); handled {
		if err != nil {
			return message, fmt.Errorf("render alarm dispatch group source: %w", err)
		}

		return message, nil
	}

	if len(group.notifications) == 1 {
		return renderAlarmDispatchNotification(ctx, renderer, messageStrings, members, &group.notifications[0])
	}

	return renderAlarmDispatchNotificationGroup(ctx, renderer, messageStrings, members, shortLinkBaseURL, group)
}

func renderAlarmDispatchGroupSource(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, members domain.MemberDataProvider, group alarmDispatchGroup) (message delivery.MessageVariants, handled bool, err error) {
	if len(group.envelopes) == 0 {
		return delivery.MessageVariants{}, false, nil
	}

	envelope := &group.envelopes[0]

	type sourceRenderer struct {
		action string
		run    func() (delivery.MessageVariants, error)
	}

	renderers := map[domain.AlarmDispatchSourceKind]sourceRenderer{
		domain.AlarmDispatchSourceKindXSpace: {
			action: "render x space start",
			run: func() (delivery.MessageVariants, error) {
				if validationErr := envelope.ValidateCanonicalDispatch(); validationErr != nil {
					return delivery.MessageVariants{}, fmt.Errorf("validate x space: %w", validationErr)
				}

				memberName, nameErr := alarmContractMemberName(ctx, messageStrings, members, envelope.XSpace.ChannelID)
				if nameErr != nil {
					return delivery.MessageVariants{}, fmt.Errorf("resolve x space member name: %w", nameErr)
				}

				return renderTemplateVariants(ctx, renderer, domain.TemplateKeyXSpaceStarted, xSpaceStartView{
					MemberName: memberName, Title: envelope.XSpace.Title, URL: envelope.XSpace.URL(),
				})
			},
		},
		domain.AlarmDispatchSourceKindCelebration: {
			action: "render celebration message",
			run:    func() (delivery.MessageVariants, error) { return renderCelebrationMessage(ctx, renderer, envelope) },
		},
		domain.AlarmDispatchSourceKindDeliveryDigest: {
			action: "render delivery digest dispatch",
			run:    func() (delivery.MessageVariants, error) { return renderAlarmDispatchDeliveryDigest(envelope) },
		},
	}

	selected, ok := renderers[envelope.SourceKind]
	if !ok {
		return delivery.MessageVariants{}, false, nil
	}

	message, err = selected.run()
	if err != nil {
		return message, true, fmt.Errorf("%s: %w", selected.action, err)
	}

	return message, true, nil
}

// 저장된 digest envelope는 변형 도입 전에 한 본문만 담았으므로 그 본문을 두 경로에 그대로 쓴다. 이 저장소에는 새 digest
// 생산자가 없으므로 남은 envelope가 모두 처리되면 이 분기와 함께 지울 수 있다.
func renderAlarmDispatchDeliveryDigest(envelope *dispatchoutbox.AlarmQueueEnvelope) (delivery.MessageVariants, error) {
	if envelope.DeliveryDigest == nil {
		return delivery.MessageVariants{}, errors.New("payload is nil")
	}

	message := envelope.DeliveryDigest.PreRenderedMessage

	return delivery.MessageVariants{Plain: message, RichText: message}, nil
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

func buildAlarmDispatchItemView(ctx context.Context, store *messagestrings.Store, members *alarmDispatchMembers, notification *domain.AlarmNotification, groupMinutesUntil int) (alarmDispatchItemView, error) {
	starting := notification.IsStarting()

	collabMembers, err := formatAlarmDispatchCollabMembers(ctx, members, notification.Stream)
	if err != nil {
		return alarmDispatchItemView{}, err
	}

	memberName, err := resolveAlarmDispatchMemberName(ctx, store, members.provider, notification)
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
// 콜라보 이름이 없으면 멤버를 적재하지 않는다.
func formatAlarmDispatchCollabMembers(ctx context.Context, members *alarmDispatchMembers, stream *domain.Stream) (string, error) {
	if stream == nil || len(stream.CollaboTalentNames) == 0 {
		return "", nil
	}

	index, err := members.collabIndex(ctx)
	if err != nil {
		return "", fmt.Errorf("format collab members: %w", err)
	}

	names, err := index.DisplayNames(ctx, members.provider, stream.CollaboTalentNames, stream.ChannelID)
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

func buildAlarmDispatchGroupViewWithShortLinks(
	ctx context.Context,
	store *messagestrings.Store,
	members *alarmDispatchMembers,
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

func renderAlarmDispatchNotificationGroup(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members *alarmDispatchMembers, shortLinkBaseURL string, group alarmDispatchGroup) (delivery.MessageVariants, error) {
	shortLinks, err := configuredAlarmShortLinkBuilder(shortLinkBaseURL)
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render alarm dispatch notification group: short links: %w", err)
	}

	view, err := buildAlarmDispatchGroupViewWithShortLinks(ctx, store, members, group, shortLinks)
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	message, err := renderTemplateVariants(ctx, renderer, domain.TemplateKeyAlarmDispatchNotificationGroup, view)
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render alarm dispatch notification group: %w", err)
	}

	return message, nil
}

func renderAlarmDispatchNotification(ctx context.Context, renderer *template.Renderer, store *messagestrings.Store, members *alarmDispatchMembers, notification *domain.AlarmNotification) (delivery.MessageVariants, error) {
	view, err := buildAlarmDispatchItemView(ctx, store, members, notification, -1)
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render alarm dispatch notification: %w", err)
	}

	message, err := renderTemplateVariants(ctx, renderer, domain.TemplateKeyAlarmDispatchNotification, view)
	if err != nil {
		return delivery.MessageVariants{}, fmt.Errorf("render alarm dispatch notification: %w", err)
	}

	return message, nil
}

// 멤버 이름은 저장된 payload가 아니라 렌더 때 members에서 정한다.
type xSpaceStartView struct {
	MemberName string
	Title      string
	URL        string
}

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

// 멤버 표시명 예외 계약(members short_korean_name→korean_name→misc/vtuber_fallback)을 따른다.
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

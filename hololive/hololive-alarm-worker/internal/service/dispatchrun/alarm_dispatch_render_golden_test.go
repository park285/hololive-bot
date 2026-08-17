package dispatchrun

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAlarmDispatchTestRenderer(t *testing.T) *template.Renderer {
	t.Helper()
	return template.NewRenderer(dbtest.NewPool(t), slog.Default())
}

func newAlarmDispatchTestRendering(t *testing.T) (*template.Renderer, *messagestrings.Store) {
	t.Helper()
	pool := dbtest.NewPool(t)
	return template.NewRenderer(pool, slog.Default()), messagestrings.NewStore(pool, slog.Default())
}

func goldenAlarmDispatchMember(n *domain.AlarmNotification) string {
	if n.Channel != nil && strings.TrimSpace(n.Channel.Name) != "" {
		return strings.TrimSpace(n.Channel.Name)
	}
	if n.Stream != nil && strings.TrimSpace(n.Stream.ChannelName) != "" {
		return strings.TrimSpace(n.Stream.ChannelName)
	}
	return "알 수 없는 멤버"
}

func goldenAlarmDispatchTitle(n *domain.AlarmNotification) string {
	if n.Stream == nil {
		return "방송 정보 없음"
	}
	if title := strings.TrimSpace(n.Stream.Title); title != "" {
		return title
	}
	return "제목 없음"
}

func goldenAlarmDispatchNotificationIsStarting(n *domain.AlarmNotification) bool {
	if n == nil {
		return false
	}
	if n.MinutesUntil <= 0 {
		return true
	}
	if n.Stream == nil {
		return false
	}
	return n.Stream.Status == domain.StreamStatusLive || n.Stream.StartActual != nil
}

func goldenAlarmDispatchItem(n *domain.AlarmNotification, groupMinutesUntil int) string {
	member := util.MarkdownNeutralize(goldenAlarmDispatchMember(n))
	title := util.MarkdownNeutralize(goldenAlarmDispatchTitle(n))
	url := resolveAlarmDispatchURL(n)
	label := "방송"
	if n.Stream != nil && n.Stream.IsPremiere {
		label = "선행공개"
	}
	var b strings.Builder
	if groupMinutesUntil < 0 {
		b.WriteString("## ")
	}
	switch {
	case goldenAlarmDispatchNotificationIsStarting(n):
		fmt.Fprintf(&b, "🔴 **%s** %s 시작", member, label)
	case groupMinutesUntil > 0 && n.MinutesUntil == groupMinutesUntil:
		fmt.Fprintf(&b, "⏰ **%s** %s 예정", member, label)
	default:
		fmt.Fprintf(&b, "⏰ **%s** %s %d분 전", member, label, n.MinutesUntil)
	}
	linkable := title != "" && url != "" && !strings.Contains(url, " | ")
	if linkable {
		fmt.Fprintf(&b, "\n[%s](%s)", title, url)
	} else if title != "" {
		fmt.Fprintf(&b, "\n%s%s", util.KakaoZeroWidthSpace, title)
	}
	if collabMembers := formatAlarmDispatchCollabMembers(nil, n.Stream); collabMembers != "" {
		fmt.Fprintf(&b, "\n콜라보: %s", util.MarkdownNeutralize(collabMembers))
	}
	if scheduleMessage := strings.TrimSpace(n.ScheduleChangeMessage); scheduleMessage != "" {
		fmt.Fprintf(&b, "\n%s%s", util.KakaoZeroWidthSpace, util.MarkdownNeutralize(scheduleMessage))
	}
	if url != "" && !linkable {
		fmt.Fprintf(&b, "\n%s", url)
	}
	return b.String()
}

func goldenAlarmDispatchGroupAllStarting(group alarmDispatchGroup) bool {
	if len(group.notifications) == 0 {
		return group.minutesUntil <= 0
	}
	for i := range group.notifications {
		if !goldenAlarmDispatchNotificationIsStarting(&group.notifications[i]) {
			return false
		}
	}
	return true
}

func goldenAlarmDispatchGroupAllPremiere(group alarmDispatchGroup) bool {
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

func goldenAlarmDispatchGroup(group alarmDispatchGroup) string {
	label := "방송"
	if goldenAlarmDispatchGroupAllPremiere(group) {
		label = "선행공개"
	}
	var b strings.Builder
	if goldenAlarmDispatchGroupAllStarting(group) {
		fmt.Fprintf(&b, "## 🔴 %s 시작", label)
	} else {
		fmt.Fprintf(&b, "## ⏰ %s %d분 전", label, group.minutesUntil)
	}
	for i := range group.notifications {
		b.WriteString("\n\n")
		b.WriteString(goldenAlarmDispatchItem(&group.notifications[i], group.minutesUntil))
	}
	return b.String()
}

func alarmGoldenStream(id, title string) *domain.Stream {
	return &domain.Stream{ID: id, Title: title}
}

func alarmGoldenNotification(name string, minutesUntil int, stream *domain.Stream) domain.AlarmNotification {
	var channel *domain.Channel
	if name != "" {
		channel = &domain.Channel{Name: name}
	}
	return domain.AlarmNotification{
		AlarmType:    domain.AlarmTypeLive,
		RoomID:       "room-golden",
		Channel:      channel,
		Stream:       stream,
		MinutesUntil: minutesUntil,
	}
}

func TestBuildAlarmDispatchPremiereViews(t *testing.T) {
	premiere := alarmGoldenNotification("Premiere", 5, alarmGoldenStream("premiere", "Premiere Title"))
	premiere.Stream.IsPremiere = true
	regular := alarmGoldenNotification("Regular", 5, alarmGoldenStream("regular", "Regular Title"))

	item := buildAlarmDispatchItemView(t.Context(), nil, nil, &premiere, 5)
	assert.True(t, item.IsPremiere)

	allPremiere := buildAlarmDispatchGroupView(t.Context(), nil, nil, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{premiere, premiere},
	})
	assert.True(t, allPremiere.AllPremiere)
	require.Len(t, allPremiere.Entries, 2)
	assert.True(t, allPremiere.Entries[0].IsPremiere)
	assert.True(t, allPremiere.Entries[1].IsPremiere)

	mixed := buildAlarmDispatchGroupView(t.Context(), nil, nil, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{premiere, regular},
	})
	assert.False(t, mixed.AllPremiere)
	require.Len(t, mixed.Entries, 2)
	assert.True(t, mixed.Entries[0].IsPremiere)
	assert.False(t, mixed.Entries[1].IsPremiere)
}

func TestRenderAlarmDispatchNotificationMatchesCanonicalRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	twitch := alarmGoldenStream("tw-1", "Twitch 방송")
	twitch.IsTwitchOnly = true
	twitch.TwitchLiveURL = "https://twitch.tv/holomember"

	chzzk := alarmGoldenStream("cz-1", "치지직 방송")
	chzzk.IsChzzkOnly = true
	chzzk.ChzzkLiveURL = "https://chzzk.naver.com/live/abcdef"

	integrated := alarmGoldenStream("yt-int", "동시송출 방송")
	integrated.IsIntegrated = true
	integrated.ChzzkLiveURL = "https://chzzk.naver.com/live/zzz"

	liveStatus := alarmGoldenStream("yt-live-status", "LIVE 상태 방송")
	liveStatus.Status = domain.StreamStatusLive

	startActual := alarmGoldenStream("yt-start-actual", "실제 시작 방송")
	startActual.StartActual = &start

	upcomingStatus := alarmGoldenStream("yt-upcoming", "예정 방송")
	upcomingStatus.Status = domain.StreamStatusUpcoming

	premiere := alarmGoldenStream("yt-premiere", "선행공개 제목")
	premiere.Status = domain.StreamStatusUpcoming
	premiere.IsPremiere = true

	cases := []struct {
		name         string
		notification domain.AlarmNotification
	}{
		{"single-start-youtube", alarmGoldenNotification("스이세이", 0, alarmGoldenStream("yt-1", "방송 제목"))},
		{"single-nbefore-youtube", alarmGoldenNotification("스이세이", 5, alarmGoldenStream("yt-1", "방송 제목"))},
		{"single-live-status-youtube", alarmGoldenNotification("스이세이", 5, liveStatus)},
		{"single-start-actual-youtube", alarmGoldenNotification("스이세이", 5, startActual)},
		{"single-upcoming-status-youtube", alarmGoldenNotification("스이세이", 5, upcomingStatus)},
		{"single-premiere-youtube", alarmGoldenNotification("스이세이", 5, premiere)},
		{"single-twitch-only", alarmGoldenNotification("멤버", 3, twitch)},
		{"single-chzzk-only", alarmGoldenNotification("멤버", 3, chzzk)},
		{"single-integrated", alarmGoldenNotification("멤버", 3, integrated)},
		{"placeholder-member", alarmGoldenNotification("", 0, alarmGoldenStream("yt-2", "방송"))},
		{"placeholder-title", alarmGoldenNotification("멤버", 0, alarmGoldenStream("yt-3", ""))},
		{"placeholder-stream-nil", alarmGoldenNotification("멤버", 0, nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notification := tc.notification
			want := goldenAlarmDispatchItem(&notification, -1)

			got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, nil, &notification)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRenderAlarmDispatchNotificationPreservesScheduleMessageFormatting(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	with := alarmGoldenNotification("멤버", 5, alarmGoldenStream("yt-4", "방송 제목"))
	with.ScheduleChangeMessage = "  방송 시간이 21:00으로 변경되었습니다  "

	without := alarmGoldenNotification("멤버", 5, alarmGoldenStream("yt-4", "방송 제목"))
	without.ScheduleChangeMessage = "   "

	for _, tc := range []struct {
		name         string
		notification domain.AlarmNotification
	}{
		{"schedule-present-trimmed", with},
		{"schedule-blank-omitted", without},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notification := tc.notification
			want := goldenAlarmDispatchItem(&notification, -1)

			got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, nil, &notification)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRenderAlarmDispatchNotificationGroupMatchesCanonicalRendering(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	scheduled := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	startingA := alarmGoldenNotification("Member1", 0, alarmGoldenStream("s-a", "Title A"))
	startingB := alarmGoldenNotification("Member2", 0, alarmGoldenStream("s-b", "Title B"))

	catchupA := alarmGoldenNotification("Member1", 5, alarmGoldenStream("c-a", "Title A"))
	catchupA.Stream.StartActual = &scheduled
	catchupB := alarmGoldenNotification("Member2", 5, alarmGoldenStream("c-b", "Title B"))
	catchupB.Stream.StartActual = &scheduled

	mixedA := alarmGoldenNotification("Member1", 3, alarmGoldenStream("m-a", "Title1"))
	mixedA.Stream.StartScheduled = &scheduled
	mixedB := alarmGoldenNotification("Member2", 1, alarmGoldenStream("m-b", "Title2"))
	mixedB.Stream.StartScheduled = &scheduled
	mixedB.ScheduleChangeMessage = "변경 안내"

	premiereA := alarmGoldenNotification("Premiere1", 5, alarmGoldenStream("p-a", "Premiere A"))
	premiereA.Stream.IsPremiere = true
	premiereB := alarmGoldenNotification("Premiere2", 5, alarmGoldenStream("p-b", "Premiere B"))
	premiereB.Stream.IsPremiere = true

	mixedPremiere := premiereA
	mixedRegular := alarmGoldenNotification("Regular", 5, alarmGoldenStream("p-regular", "Regular"))

	cases := []struct {
		name  string
		group alarmDispatchGroup
	}{
		{
			name: "group-start",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  0,
				notifications: []domain.AlarmNotification{startingA, startingB},
			},
		},
		{
			name: "group-all-live-catchup",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  5,
				notifications: []domain.AlarmNotification{catchupA, catchupB},
			},
		},
		{
			name: "group-nbefore-scheduled-and-countdown",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  1,
				notifications: []domain.AlarmNotification{mixedA, mixedB},
			},
		},
		{
			name: "group-all-premiere",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  5,
				notifications: []domain.AlarmNotification{premiereA, premiereB},
			},
		},
		{
			name: "group-mixed-premiere",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  5,
				notifications: []domain.AlarmNotification{mixedPremiere, mixedRegular},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenAlarmDispatchGroup(tc.group)

			got, err := renderAlarmDispatchNotificationGroup(t.Context(), renderer, store, nil, tc.group)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRenderAlarmDispatchPlaceholderResolvesFromMessageStrings(t *testing.T) {
	_, store := newAlarmDispatchTestRendering(t)

	require.NoError(t, store.Load(t.Context()))
	assert.Equal(t, "알 수 없는 멤버", store.Get(messagestrings.NamespaceMisc, "alarm_unknown_member"))
	assert.Equal(t, "제목 없음", store.Get(messagestrings.NamespaceMisc, "alarm_no_title"))
	assert.Equal(t, "방송 정보 없음", store.Get(messagestrings.NamespaceMisc, "alarm_no_stream"))
}

func TestRenderAlarmDispatchNotificationIncludesCollabMembers(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	notification := alarmGoldenNotification("미코", 5, alarmGoldenStream("collab-1", "콜라보 방송"))
	notification.Stream.ChannelID = "ch-miko"
	notification.Stream.CollaboTalentNames = []string{"星街すいせい", "Gawr Gura"}

	got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, collabTestMembers{}, &notification)
	require.NoError(t, err)
	assert.Contains(t, got, "콜라보: 스이세이, Gawr Gura")
}

type collabTestMembers struct{}

func (m collabTestMembers) GetAllMembers() []*domain.Member {
	return []*domain.Member{
		{
			Name:            "Hoshimachi Suisei",
			NameJa:          "星街すいせい",
			ShortKoreanName: "스이세이",
			ChannelID:       "ch-sui",
		},
	}
}

func (m collabTestMembers) FindMemberByChannelID(channelID string) *domain.Member {
	for _, member := range m.GetAllMembers() {
		if member.ChannelID == channelID {
			return member
		}
	}
	return nil
}

func (collabTestMembers) FindMemberByName(string) *domain.Member  { return nil }
func (collabTestMembers) FindMemberByAlias(string) *domain.Member { return nil }
func (collabTestMembers) GetChannelIDs() []string                 { return nil }
func (m collabTestMembers) WithContext(context.Context) domain.MemberDataProvider {
	return m
}
func (collabTestMembers) FindMembersByName(string) []*domain.Member  { return nil }
func (collabTestMembers) FindMembersByAlias(string) []*domain.Member { return nil }

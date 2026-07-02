package workerapp

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
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

func goldenAlarmDispatchItem(n *domain.AlarmNotification, groupMinutesUntil int) string {
	member := goldenAlarmDispatchMember(n)
	title := goldenAlarmDispatchTitle(n)
	url := resolveAlarmDispatchURL(n)
	var b strings.Builder
	switch {
	case n.MinutesUntil <= 0:
		fmt.Fprintf(&b, "🔴 %s 방송 시작", member)
	case groupMinutesUntil > 0 && n.MinutesUntil == groupMinutesUntil:
		fmt.Fprintf(&b, "⏰ %s 방송 예정", member)
	default:
		fmt.Fprintf(&b, "⏰ %s 방송 %d분 전", member, n.MinutesUntil)
	}
	fmt.Fprintf(&b, "\n  %s", title)
	if scheduleMessage := strings.TrimSpace(n.ScheduleChangeMessage); scheduleMessage != "" {
		fmt.Fprintf(&b, "\n  %s", scheduleMessage)
	}
	if url != "" {
		fmt.Fprintf(&b, "\n  %s", url)
	}
	return b.String()
}

func goldenAlarmDispatchGroup(group alarmDispatchGroup) string {
	var b strings.Builder
	if group.minutesUntil <= 0 {
		b.WriteString("🔴 방송 시작")
	} else {
		fmt.Fprintf(&b, "⏰ 방송 %d분 전", group.minutesUntil)
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

func TestRenderAlarmDispatchNotificationByteEqualsLegacyHardcoded(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	twitch := alarmGoldenStream("tw-1", "Twitch 방송")
	twitch.IsTwitchOnly = true
	twitch.TwitchLiveURL = "https://twitch.tv/holomember"

	chzzk := alarmGoldenStream("cz-1", "치지직 방송")
	chzzk.IsChzzkOnly = true
	chzzk.ChzzkLiveURL = "https://chzzk.naver.com/live/abcdef"

	integrated := alarmGoldenStream("yt-int", "동시송출 방송")
	integrated.IsIntegrated = true
	integrated.ChzzkLiveURL = "https://chzzk.naver.com/live/zzz"

	cases := []struct {
		name         string
		notification domain.AlarmNotification
	}{
		{"single-start-youtube", alarmGoldenNotification("스이세이", 0, alarmGoldenStream("yt-1", "방송 제목"))},
		{"single-nbefore-youtube", alarmGoldenNotification("스이세이", 5, alarmGoldenStream("yt-1", "방송 제목"))},
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

			got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, &notification)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRenderAlarmDispatchNotificationScheduleMessageByteEqualsLegacy(t *testing.T) {
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

			got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, &notification)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestRenderAlarmDispatchNotificationGroupByteEqualsLegacyHardcoded(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	scheduled := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	startingA := alarmGoldenNotification("Member1", 0, alarmGoldenStream("s-a", "Title A"))
	startingB := alarmGoldenNotification("Member2", 0, alarmGoldenStream("s-b", "Title B"))

	mixedA := alarmGoldenNotification("Member1", 3, alarmGoldenStream("m-a", "Title1"))
	mixedA.Stream.StartScheduled = &scheduled
	mixedB := alarmGoldenNotification("Member2", 1, alarmGoldenStream("m-b", "Title2"))
	mixedB.Stream.StartScheduled = &scheduled
	mixedB.ScheduleChangeMessage = "변경 안내"

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
			name: "group-nbefore-scheduled-and-countdown",
			group: alarmDispatchGroup{
				roomID:        "room-golden",
				minutesUntil:  1,
				notifications: []domain.AlarmNotification{mixedA, mixedB},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenAlarmDispatchGroup(tc.group)

			got, err := renderAlarmDispatchNotificationGroup(t.Context(), renderer, store, tc.group)

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

package alarmdispatch

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/kakaotext"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func newAlarmDispatchTestRenderer(t *testing.T) *template.Renderer {
	t.Helper()

	return template.NewRenderer(dbtest.NewPool(t), slog.Default())
}

// newAlarmDispatchTestMessageStrings는 시드 message_strings를 적재한 store다. 코드 대체 문구가 없으므로 알림
// 문구와 placeholder는 DB 정본에서만 나온다.
func newAlarmDispatchTestMessageStrings(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	require.NoError(t, store.Load(t.Context()))

	return store
}

func newAlarmDispatchTestRendering(t *testing.T) (*template.Renderer, *messagestrings.Store) {
	t.Helper()

	pool := dbtest.NewPool(t)
	store := messagestrings.NewStore(pool, slog.Default())
	require.NoError(t, store.Load(t.Context()))

	return template.NewRenderer(pool, slog.Default()), store
}

// alarmGoldenChannelPrefix 뒤의 이름이 members 정본 표시명이다. 원천 채널 제목(Channel.Name)은 표시하지 않는다.
const alarmGoldenChannelPrefix = "ch-golden-"

// alarmGoldenMembers는 golden 알림 채널을 짧은 한국어 표시명만 가진 멤버로 찾는다.
type alarmGoldenMembers struct {
	collabTestMembers
}

func (alarmGoldenMembers) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	name, ok := strings.CutPrefix(channelID, alarmGoldenChannelPrefix)
	if !ok || name == "" {
		return nil, domain.ErrMemberNotFound
	}

	return &domain.Member{ChannelID: channelID, Name: "English " + name, ShortKoreanName: name}, nil
}

func alarmGoldenStream(id, title string) *domain.Stream {
	return &domain.Stream{ID: id, Title: title}
}

func alarmGoldenNotification(name string, stream *domain.Stream) domain.AlarmNotification {
	var channel *domain.Channel

	if name != "" {
		channel = &domain.Channel{ID: alarmGoldenChannelPrefix + name, Name: name + " Ch."}
	}

	return domain.AlarmNotification{
		AlarmType:    domain.AlarmTypeLive,
		RoomID:       testAlarmGoldenRoomID,
		Channel:      channel,
		Stream:       stream,
		MinutesUntil: 5,
	}
}

func TestBuildAlarmDispatchPremiereViews(t *testing.T) {
	premiere := alarmGoldenNotification("Premiere", alarmGoldenStream("premiere", "Premiere Title"))

	premiere.Stream.IsPremiere = true

	regular := alarmGoldenNotification("Regular", alarmGoldenStream("regular", "Regular Title"))

	item, err := buildAlarmDispatchItemView(t.Context(), nil, alarmGoldenMembers{}, &premiere, 5)
	require.NoError(t, err)
	assert.True(t, item.IsPremiere)

	allPremiere, err := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, alarmGoldenMembers{}, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{premiere, premiere},
	}, shortlinkservice.YouTubeBuilder{})
	require.NoError(t, err)
	assert.True(t, allPremiere.AllPremiere)
	require.Len(t, allPremiere.Entries, 2)
	assert.True(t, allPremiere.Entries[0].IsPremiere)
	assert.True(t, allPremiere.Entries[1].IsPremiere)

	mixed, err := buildAlarmDispatchGroupViewWithShortLinks(t.Context(), nil, alarmGoldenMembers{}, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{premiere, regular},
	}, shortlinkservice.YouTubeBuilder{})
	require.NoError(t, err)
	assert.False(t, mixed.AllPremiere)
	require.Len(t, mixed.Entries, 2)
	assert.True(t, mixed.Entries[0].IsPremiere)
	assert.False(t, mixed.Entries[1].IsPremiere)
}

func TestRenderAlarmDispatchNotificationIncludesCollabMembers(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	notification := alarmGoldenNotification("미코", alarmGoldenStream("collab-1", "콜라보 방송"))

	notification.Stream.ChannelID = "ch-miko"
	notification.Stream.CollaboTalentNames = []string{"星街すいせい", "Gawr Gura"}

	got, err := renderAlarmDispatchNotification(t.Context(), renderer, store, collabTestMembers{}, &notification)
	require.NoError(t, err)
	assert.Contains(t, got, "스이세이")
	assert.Contains(t, got, "Gawr Gura")
}

type collabTestMembers struct{}

func collabTestMemberList() []*domain.Member {
	return []*domain.Member{
		{
			Name:            "Hoshimachi Suisei",
			NameJa:          "星街すいせい",
			ShortKoreanName: "스이세이",
			ChannelID:       "ch-sui",
		},
	}
}

func (collabTestMembers) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return collabTestMemberList(), nil
}

func (collabTestMembers) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	for _, member := range collabTestMemberList() {
		if member.ChannelID == channelID {
			return member, nil
		}
	}

	return nil, domain.ErrMemberNotFound
}

func (collabTestMembers) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (collabTestMembers) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}
func (collabTestMembers) GetChannelIDs(context.Context) ([]string, error) { return nil, nil }
func (collabTestMembers) FindMembersByName(context.Context, string) ([]*domain.Member, error) {
	return nil, nil
}

func (collabTestMembers) FindMembersByAlias(context.Context, string) ([]*domain.Member, error) {
	return nil, nil
}

func TestAlarmProducerPreservesLiteralFields(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)

	const (
		member = "||멤버|| **끝**"
		title  = "1. ||가격|| **제목** _원문_"
		note   = "> 변경 **안내**"
	)

	notification := alarmGoldenNotification(member, alarmGoldenStream("a_b-c", title))

	notification.ScheduleChangeMessage = note

	single, err := renderAlarmDispatchNotification(t.Context(), renderer, store, alarmGoldenMembers{}, &notification)
	require.NoError(t, err)

	grouped, err := renderAlarmDispatchNotificationGroup(t.Context(), renderer, store, alarmGoldenMembers{}, "", alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{notification, notification},
	})
	require.NoError(t, err)

	for _, message := range []string{single, grouped} {
		visible := strings.ReplaceAll(kakaoformat.Render(message), kakaotext.ZeroWidthSpace, "")

		for _, field := range []string{member, "||가격||", "**제목**", "_원문_", note} {
			require.Contains(t, visible, field)
		}

		require.Contains(t, visible, "https://")
		require.Contains(t, visible, "a_b-c")
	}
}

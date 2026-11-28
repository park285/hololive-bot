package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

type noticeStoreStub struct {
	queue     []scheduleposter.Notice
	current   bool
	alsoPosts []int64
	schedule  scheduleposter.PublishedSchedule
	found     bool
	posterIDs []int64
	settled   []noticeSettlement
}

type noticeSettlement struct {
	id     int64
	status scheduleposter.NoticeStatus
	rooms  int
}

func (s *noticeStoreStub) ClaimNotice(context.Context, time.Duration) (scheduleposter.Notice, bool, error) {
	if len(s.queue) == 0 {
		return scheduleposter.Notice{}, false, nil
	}

	notice := s.queue[0]

	s.queue = s.queue[1:]

	return notice, true, nil
}

func (s *noticeStoreStub) MemberPosterRows(_ context.Context, posterID int64, _ int, _ string, _ time.Time) (scheduleposter.PublishedSchedule, cardtemplate.ID, bool, error) {
	s.posterIDs = append(s.posterIDs, posterID)

	return s.schedule, cardtemplate.Classic, s.found, nil
}

func (s *noticeStoreStub) NoticeChangePosts(_ context.Context, notice scheduleposter.Notice) ([]int64, error) {
	if !s.current {
		return s.alsoPosts, nil
	}

	return append([]int64{notice.PostID}, s.alsoPosts...), nil
}

func (s *noticeStoreStub) SettleNotice(_ context.Context, notice scheduleposter.Notice, status scheduleposter.NoticeStatus, rooms int) error {
	s.settled = append(s.settled, noticeSettlement{id: notice.ID, status: status, rooms: rooms})

	return nil
}

type liveRoomsStub struct {
	rooms []string
	calls int
}

func (s *liveRoomsStub) LiveSubscriberRooms(context.Context, string) ([]string, error) {
	s.calls++

	return s.rooms, nil
}

type noticeSend struct {
	room, identity, message string
	images                  int
}

func scheduleNoticeFixture(t *testing.T, notice scheduleposter.Notice, rooms []string, failRoom string) (*SchedulePosterNotifier, *noticeStoreStub, *scheduleCardStub, *liveRoomsStub, *[]noticeSend) {
	t.Helper()

	store := &noticeStoreStub{queue: []scheduleposter.Notice{notice}, current: true, schedule: publishedMikoSchedule(), found: true}
	cards := &scheduleCardStub{data: [][]byte{[]byte("png")}}
	lister := &liveRoomsStub{rooms: rooms}
	sends := &[]noticeSend{}

	deps := schedulePosterDeps(t, nil, cards, &posterReplies{}, nil)

	deps.MembersData = newContextAwareMemberProvider([]*domain.Member{{ID: 37, ChannelID: testChannelMiko, Name: "Miko", NameKo: "사쿠라 미코", ShortKoreanName: "미코"}})
	deps.SendMessage = func(ctx context.Context, room, message string) error {
		identity, _ := transport.ReplyIdentityFromContext(ctx)

		*sends = append(*sends, noticeSend{room: room, identity: identity, message: message})

		if room == failRoom {
			return errors.New("outbox unavailable")
		}

		return nil
	}
	deps.SendImages = func(ctx context.Context, room string, images [][]byte) error {
		identity, _ := transport.ReplyIdentityFromContext(ctx)

		*sends = append(*sends, noticeSend{room: room, identity: identity, images: len(images)})

		return nil
	}

	notifier := NewSchedulePosterNotifier(deps, store, lister)
	require.NoError(t, notifier.CheckDependencies())

	return notifier, store, cards, lister, sends
}

func claimedNotice(kind scheduleposter.NoticeKind) scheduleposter.Notice {
	at := time.Date(2026, time.November, 10, 3, 0, 0, 0, time.UTC)

	notice := scheduleposter.Notice{ID: 9, Kind: kind, PosterID: 7, ChannelID: testChannelMiko, CreatedAt: at, ClaimedAt: at.Add(time.Minute)}
	if kind == scheduleposter.NoticeChanged {
		notice.PostID = 2054825907916054528
	}

	return notice
}

func TestScheduleNoticeSendsOneRenderedCardToEveryLiveRoom(t *testing.T) {
	notifier, store, cards, _, sends := scheduleNoticeFixture(t, claimedNotice(scheduleposter.NoticePublished), []string{"100", "200"}, "")

	store.schedule.RowsDayShift = 1

	handled, err := notifier.NotifyNext(t.Context())
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, 1, cards.calls, "모든 방이 같은 그림을 받도록 한 번만 그린다")
	require.Nil(t, cards.changed, "새 일정표 알림은 표시 없는 원본 카드를 보낸다")
	require.Equal(t, []int64{7}, store.posterIDs)
	require.Equal(t, []noticeSettlement{{id: 9, status: scheduleposter.NoticeSent, rooms: 2}}, store.settled)

	headline := (*sends)[0].message
	require.True(t, strings.HasPrefix(headline, "📅 ") && strings.HasSuffix(headline, " 새 주간 일정표 (11/10 ~ 11/16)"), headline, "카드와 같은 주 범위를 쓴다")
	require.Equal(t, []noticeSend{
		{room: "100", identity: "schedule-notice:9:100", message: headline},
		{room: "100", identity: "schedule-notice:9:100", images: 1},
		{room: "200", identity: "schedule-notice:9:200", message: headline},
		{room: "200", identity: "schedule-notice:9:200", images: 1},
	}, *sends)

	handled, err = notifier.NotifyNext(t.Context())
	require.NoError(t, err)
	require.False(t, handled)
}

func TestScheduleNoticeKeepsSendingToOtherRoomsAndStaysPendingAfterARoomFails(t *testing.T) {
	notifier, store, _, _, sends := scheduleNoticeFixture(t, claimedNotice(scheduleposter.NoticePublished), []string{"100", "200", "300"}, "200")

	_, err := notifier.NotifyNext(t.Context())
	require.ErrorContains(t, err, "room "+sharedprivacylog.Pseudonym("200"))
	require.NotContains(t, err.Error(), "room 200")
	require.Empty(t, store.settled, "실패한 방이 있으면 임차가 끝난 뒤 다시 보낸다")

	imageRooms := []string{}

	for _, send := range *sends {
		if send.images > 0 {
			imageRooms = append(imageRooms, send.room)
		}
	}

	require.Equal(t, []string{"100", "300"}, imageRooms)
}

func TestScheduleNoticeAnnouncesAnAppliedChangeWithThePost(t *testing.T) {
	notifier, store, cards, _, sends := scheduleNoticeFixture(t, claimedNotice(scheduleposter.NoticeChanged), []string{"100"}, "")

	store.alsoPosts = []int64{2054825907916054600}

	_, err := notifier.NotifyNext(t.Context())
	require.NoError(t, err)
	require.Equal(t, []int64{7}, store.posterIDs, "변경 알림도 알림의 포스터를 그린다")
	require.Equal(t, []int64{2054825907916054528, 2054825907916054600}, cards.changed, "변경 알림 카드는 함께 닫히는 변경까지 표시한다")
	require.True(t, strings.HasSuffix((*sends)[0].message, " 일정이 변경되었습니다.\nhttps://x.com/i/status/2054825907916054528"), (*sends)[0].message)
	require.Equal(t, []noticeSettlement{{id: 9, status: scheduleposter.NoticeSent, rooms: 1}}, store.settled)
}

func TestScheduleNoticeSkipsWithoutSendingWhenTheNoticeNoLongerHolds(t *testing.T) {
	expired := claimedNotice(scheduleposter.NoticePublished)

	expired.ClaimedAt = expired.CreatedAt.Add(scheduleNoticeMaxAge + time.Minute)

	unknown := claimedNotice(scheduleposter.NoticePublished)

	unknown.ChannelID = "UC_unknown"

	for name, tt := range map[string]struct {
		notice    scheduleposter.Notice
		current   bool
		alsoPosts []int64
		found     bool
	}{
		"expired":           {notice: expired, current: true, found: true},
		"unknown member":    {notice: unknown, current: true, found: true},
		"change superseded": {notice: claimedNotice(scheduleposter.NoticeChanged), current: false, found: true},
		"only other changes current": {
			notice: claimedNotice(scheduleposter.NoticeChanged), current: false, alsoPosts: []int64{2054825907916054600}, found: true,
		},
		"poster gone": {notice: claimedNotice(scheduleposter.NoticePublished), current: true, found: false},
	} {
		t.Run(name, func(t *testing.T) {
			notifier, store, cards, lister, sends := scheduleNoticeFixture(t, tt.notice, []string{"100"}, "")

			store.current, store.alsoPosts, store.found = tt.current, tt.alsoPosts, tt.found

			handled, err := notifier.NotifyNext(t.Context())
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, []noticeSettlement{{id: 9, status: scheduleposter.NoticeSkipped}}, store.settled)
			require.Zero(t, cards.calls)
			require.Zero(t, lister.calls)
			require.Empty(t, *sends)
		})
	}
}

type noticeOutbox struct {
	entries []transport.ReplyOutboxEntry
}

func (o *noticeOutbox) RecordReply(_ context.Context, entry *transport.ReplyOutboxEntry) error {
	o.entries = append(o.entries, *entry)

	return nil
}

func outboxKeys(t *testing.T, cards *scheduleCardStub) []string {
	t.Helper()

	notifier, _, fixtureCards, _, _ := scheduleNoticeFixture(t, claimedNotice(scheduleposter.NoticePublished), []string{"100", "200"}, "")

	*fixtureCards = *cards

	outbox := &noticeOutbox{}
	commands := transport.NewCommandTransport(outbox, nil)

	notifier.deps.SendMessage, notifier.deps.SendImages, notifier.deps.SendTextFile = commands.SendMessage, commands.SendImages, commands.SendTextFile

	_, err := notifier.NotifyNext(t.Context())
	require.NoError(t, err)

	keys := make([]string, 0, len(outbox.entries))

	for _, entry := range outbox.entries {
		require.Regexp(t, "^[A-Za-z0-9._:-]{8,160}$", entry.ClientRequestID)
		require.Equal(t, "schedule-notice:9:"+entry.Room, entry.MessageID)

		keys = append(keys, entry.Room+"/"+entry.ClientRequestID)
	}

	return keys
}

func TestScheduleNoticeOutboxKeysAreStablePerRoom(t *testing.T) {
	for name, cards := range map[string]*scheduleCardStub{
		"one page":  {data: [][]byte{[]byte("png")}},
		"two pages": {data: [][]byte{[]byte("png-1"), []byte("png-2")}},
		"text file": {err: schedulecard.ErrRenderBudget},
	} {
		t.Run(name, func(t *testing.T) {
			first := outboxKeys(t, cards)
			require.Len(t, first, 4)
			require.Equal(t, first, outboxKeys(t, cards), "다시 보내도 같은 키라 이미 넣은 방에는 두 번 가지 않는다")
		})
	}
}

func TestScheduleNoticeSettlesWithoutRenderingWhenNoRoomListens(t *testing.T) {
	notifier, store, cards, _, sends := scheduleNoticeFixture(t, claimedNotice(scheduleposter.NoticePublished), []string{}, "")

	_, err := notifier.NotifyNext(t.Context())
	require.NoError(t, err)
	require.Zero(t, cards.calls)
	require.Empty(t, *sends)
	require.Equal(t, []noticeSettlement{{id: 9, status: scheduleposter.NoticeSent}}, store.settled)
}

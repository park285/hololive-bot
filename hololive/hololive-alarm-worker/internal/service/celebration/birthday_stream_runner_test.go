package celebration

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var testKST = time.FixedZone("KST", 9*60*60)

const (
	testChannelA       = "UC_a"
	testChannelB       = "UC_b"
	testRoom1          = "room-1"
	testRoom2          = "room-2"
	testMemberName     = "Test"
	testStatusUpcoming = "UPCOMING"
	testStatusLive     = "LIVE"
	testVideoA         = "vid-a"
	testBirthdayDate   = "2026-07-10"
	testNextDate       = "2026-07-11"
	testMemberIDA      = 101
	testMemberIDB      = 202
	testBirthdayTitle  = "【#生誕祭2026】Birthday Live"
)

// testPublishedBirthdayStreams는 videoID별 저장 제목으로 기발행 birthday stream event를 만든다.
func testPublishedBirthdayStreams(titlesByVideoID map[string]string) map[string]domain.AlarmQueueEnvelope {
	events := make(map[string]domain.AlarmQueueEnvelope, len(titlesByVideoID))
	for videoID, title := range titlesByVideoID {
		events[birthdayStreamEventKey(testMemberIDA, testBirthdayDate, videoID)] = domain.AlarmQueueEnvelope{
			SourceKind: domain.AlarmDispatchSourceKindCelebration,
			Celebration: &domain.CelebrationDispatchPayload{
				Kind:        domain.CelebrationKindBirthdayStream,
				MemberID:    testMemberIDA,
				Date:        testBirthdayDate,
				VideoID:     videoID,
				StreamTitle: title,
			},
		}
	}

	return events
}

type birthdayStreamTestMemberRepo struct {
	membersByDay map[[2]int][]*domain.Member
	calls        [][2]int
	err          error
}

func (r *birthdayStreamTestMemberRepo) FindMembersWithBirthdayOn(_ context.Context, month, day int) ([]*domain.Member, error) {
	r.calls = append(r.calls, [2]int{month, day})
	return r.membersByDay[[2]int{month, day}], r.err
}

type birthdayStreamSessionQuery struct {
	channelIDs  []string
	windowStart time.Time
	windowEnd   time.Time
	seenSince   time.Time
}

type birthdayStreamTestStore struct {
	sessions                       []BirthdayStreamSession
	sessionsErr                    error
	sessionsErrQueue               []error
	sessionCalls                   []birthdayStreamSessionQuery
	listCalls                      []string
	publishedEvents                map[string]domain.AlarmQueueEnvelope
	publishedEventsErr             error
	publishedCelebrationEventCalls [][]string
	publishedCelebrationEventsErr  error
	defaultSentRooms               []string
	sentRoomsByEventKey            map[string][]string
	sentRoomsErr                   error
	sentRoomsEventKeyCalls         [][]string
}

func (s *birthdayStreamTestStore) FindBirthdaySessions(
	_ context.Context,
	channelIDs []string,
	windowStartUTC, windowEndUTC, seenSince, _ time.Time,
) ([]BirthdayStreamSession, error) {
	s.sessionCalls = append(s.sessionCalls, birthdayStreamSessionQuery{
		channelIDs:  channelIDs,
		windowStart: windowStartUTC,
		windowEnd:   windowEndUTC,
		seenSince:   seenSince,
	})
	if len(s.sessionsErrQueue) > 0 {
		err := s.sessionsErrQueue[0]

		s.sessionsErrQueue = s.sessionsErrQueue[1:]

		if err != nil {
			return nil, err
		}
	}

	return s.sessions, s.sessionsErr
}

func (s *birthdayStreamTestStore) ListPublishedBirthdayStreamEvents(
	_ context.Context,
	keyPrefix string,
) (map[string]domain.AlarmQueueEnvelope, error) {
	s.listCalls = append(s.listCalls, keyPrefix)
	if s.publishedEventsErr != nil {
		return nil, s.publishedEventsErr
	}

	events := make(map[string]domain.AlarmQueueEnvelope)

	for eventKey := range s.publishedEvents {
		if strings.HasPrefix(eventKey, keyPrefix) {
			events[eventKey] = s.publishedEvents[eventKey]
		}
	}

	return events, nil
}

func (s *birthdayStreamTestStore) FindPublishedCelebrationEvents(
	_ context.Context,
	eventKeys []string,
) (map[string]domain.AlarmQueueEnvelope, error) {
	s.publishedCelebrationEventCalls = append(
		s.publishedCelebrationEventCalls,
		append([]string(nil), eventKeys...),
	)
	if s.publishedCelebrationEventsErr != nil {
		return nil, s.publishedCelebrationEventsErr
	}

	events := make(map[string]domain.AlarmQueueEnvelope, len(eventKeys))
	for _, eventKey := range eventKeys {
		if envelope, ok := s.publishedEvents[eventKey]; ok {
			events[eventKey] = envelope
		}
	}

	return events, nil
}

func (s *birthdayStreamTestStore) FindSentRoomsByEventKeys(_ context.Context, eventKeys []string) (map[string][]string, error) {
	s.sentRoomsEventKeyCalls = append(s.sentRoomsEventKeyCalls, append([]string(nil), eventKeys...))
	if s.sentRoomsErr != nil {
		return nil, s.sentRoomsErr
	}

	roomsByEventKey := make(map[string][]string, len(eventKeys))
	for _, eventKey := range eventKeys {
		rooms, ok := s.sentRoomsByEventKey[eventKey]
		// defaultSentRooms는 생일축하 greeting의 audience만 흉내 낸다. 생일 방송 event의 sent 방은
		// 시험이 sentRoomsByEventKey로 직접 지정해야 한다.
		if !ok && !strings.HasPrefix(eventKey, "celebration:"+string(domain.CelebrationKindBirthdayStream)+":") {
			rooms = s.defaultSentRooms
		}

		if len(rooms) > 0 {
			roomsByEventKey[eventKey] = append([]string(nil), rooms...)
		}
	}

	return roomsByEventKey, nil
}

type birthdayStreamTestPublisher struct {
	batches [][]domain.AlarmQueueEnvelope
	result  dispatchoutbox.PublishBatchResult
	err     error
}

func (p *birthdayStreamTestPublisher) PublishDispatchBatch(_ context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	p.batches = append(p.batches, envelopes)
	return p.result, p.err
}

func (p *birthdayStreamTestPublisher) allEnvelopes() []domain.AlarmQueueEnvelope {
	var all []domain.AlarmQueueEnvelope

	for _, batch := range p.batches {
		all = append(all, batch...)
	}

	return all
}

func newBirthdayStreamTestRunner(
	memberRepo *birthdayStreamTestMemberRepo,
	store *birthdayStreamTestStore,
	publisher *birthdayStreamTestPublisher,
	rooms []string,
	now time.Time,
) *BirthdayStreamRunner {
	store.defaultSentRooms = append([]string(nil), rooms...)

	return &BirthdayStreamRunner{
		memberRepo:       memberRepo,
		sessions:         store,
		publisher:        publisher,
		runInterval:      30 * time.Minute,
		sessionFreshness: 30 * time.Minute,
		now:              func() time.Time { return now },
	}
}

func TestBirthdayStreamEvaluationDates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  time.Time
		want []string
	}{
		{
			name: "post-midnight tick also evaluates yesterday",
			now:  time.Date(2026, time.July, 11, 0, 5, 0, 0, testKST),
			want: []string{testBirthdayDate, testNextDate},
		},
		{
			name: "tick inside yesterday trailing window evaluates both days",
			now:  time.Date(2026, time.July, 11, 17, 29, 0, 0, testKST),
			want: []string{testBirthdayDate, testNextDate},
		},
		{
			name: "tick after yesterday trailing window evaluates single date",
			now:  time.Date(2026, time.July, 11, 17, 30, 0, 0, testKST),
			want: []string{testNextDate},
		},
		{
			name: "utc input inside trailing window evaluates both kst days",
			now:  time.Date(2026, time.July, 11, 8, 20, 0, 0, time.UTC),
			want: []string{testBirthdayDate, testNextDate},
		},
		{
			name: "utc input past trailing window evaluates single kst day",
			now:  time.Date(2026, time.July, 11, 8, 40, 0, 0, time.UTC),
			want: []string{testNextDate},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dates := birthdayStreamEvaluationDates(tt.now, 30*time.Minute)
			got := make([]string, 0, len(dates))

			for _, d := range dates {
				got = append(got, d.Format(time.DateOnly))
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBirthdayStreamRunOnceWindowBoundsAndFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	runner.sessionFreshness = 45 * time.Minute

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, store.sessionCalls, 1)

	call := store.sessionCalls[0]
	assert.Equal(t, []string{testChannelA}, call.channelIDs)
	assert.True(t, call.windowStart.Equal(time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)), "windowStart = %v", call.windowStart)
	assert.True(t, call.windowEnd.Equal(time.Date(2026, time.July, 11, 8, 0, 0, 0, time.UTC)), "windowEnd = %v", call.windowEnd)
	assert.True(t, call.seenSince.Equal(now.Add(-45*time.Minute)), "seenSince = %v", call.seenSince)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceMidnightCatchesLateFrameWithYesterdayDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 11, 0, 5, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-late",
			ChannelID:      testChannelA,
			Title:          testBirthdayTitle,
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, 14, 30, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Equal(t, [][2]int{{7, 10}, {7, 11}}, memberRepo.calls)
	require.Len(t, store.sessionCalls, 1)
	assert.True(t, store.sessionCalls[0].windowStart.Equal(time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Celebration)
	assert.Equal(t, testBirthdayDate, envelopes[0].Celebration.Date)
	assert.Equal(t, "vid-late", envelopes[0].Celebration.VideoID)
}

// WIB 생일 저녁 방송은 KST 다음 날 새벽에 시작하므로 다음 날 tick에서 생일 날짜로 발행하고 날짜를 붙여 표시한다.
func TestBirthdayStreamRunOnceTrailingWindowPublishesNextKSTDayStreamWithDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 11, 9, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-wib-evening",
			ChannelID:      testChannelA,
			Title:          "【BIRTHDAY LIVE】",
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, 16, 30, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Equal(t, [][2]int{{7, 10}, {7, 11}}, memberRepo.calls)
	require.Len(t, store.sessionCalls, 1)
	assert.True(t, store.sessionCalls[0].windowEnd.Equal(time.Date(2026, time.July, 11, 8, 0, 0, 0, time.UTC)))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Celebration)
	assert.Equal(t, testBirthdayDate, envelopes[0].Celebration.Date)
	assert.Equal(t, "07/11 01:30", envelopes[0].Celebration.ScheduledStartKST)
}

func TestBirthdayStreamRunOnceYesterdayFailureStillEvaluatesToday(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 11, 0, 5, 0, 0, testKST)
	yesterdayErr := errors.New("transient db blip")
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "Yesterday"}},
		{7, 11}: {{ID: testMemberIDB, ChannelID: testChannelB, Name: "Today"}},
	}}
	store := &birthdayStreamTestStore{
		sessionsErrQueue: []error{yesterdayErr},
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-today",
			ChannelID:      testChannelB,
			Title:          testBirthdayTitle,
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	require.ErrorIs(t, err, yesterdayErr)
	assert.Equal(t, [][2]int{{7, 10}, {7, 11}}, memberRepo.calls)
	require.Len(t, store.sessionCalls, 2)

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Celebration)
	assert.Equal(t, testNextDate, envelopes[0].Celebration.Date)
	assert.Equal(t, "vid-today", envelopes[0].Celebration.VideoID)
}

func TestBirthdayStreamRunOnceNoBirthdayNoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(&birthdayStreamTestMemberRepo{}, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, store.sessionCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceLiveSessionIncluded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 21, 30, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, ShortKoreanName: "후부키"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:   "vid-live",
			ChannelID: testChannelA,
			Title:     "guerrilla birthday live",
			Status:    testStatusLive,
			StartedAt: new(time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)

	payload := envelopes[0].Celebration
	require.NotNil(t, payload)
	assert.Equal(t, "vid-live", payload.VideoID)
	assert.Equal(t, "https://youtube.com/watch?v=vid-live", payload.StreamURL)
	assert.Equal(t, "21:00", payload.ScheduledStartKST)
}

// 생일 전날 KST 23시에 시작해 생일축하 발송 뒤에도 진행 중인 카운트다운은 앞쪽 창에서 날짜를 붙여 알린다.
func TestBirthdayStreamRunOnceLeadingWindowCountdownShowsPreviousDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 0, 30, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:   "vid-countdown",
			ChannelID: testChannelA,
			Title:     "【誕生日カウントダウン】",
			Status:    testStatusLive,
			StartedAt: new(time.Date(2026, time.July, 9, 14, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, store.sessionCalls, 1)
	assert.True(t, store.sessionCalls[0].windowStart.Equal(time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC)))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	assert.Equal(t, testBirthdayDate, envelopes[0].Celebration.Date)
	assert.Equal(t, "07/09 23:00", envelopes[0].Celebration.ScheduledStartKST)
}

func TestSelectBirthdayStreamSessionsWithinDailyCap(t *testing.T) {
	t.Parallel()

	session := func(videoID, title string, hour int) BirthdayStreamSession {
		return BirthdayStreamSession{
			VideoID:        videoID,
			ChannelID:      testChannelA,
			Title:          title,
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, hour, 0, 0, 0, time.UTC)),
		}
	}
	freeChat := "【雑談】Free chat"

	tests := []struct {
		name      string
		sessions  []BirthdayStreamSession
		published map[string]string
		want      []string
	}{
		{
			name:      "three classified events leave no room despite unclassified ones",
			sessions:  []BirthdayStreamSession{session("vid-new", testBirthdayTitle, 9)},
			published: map[string]string{"vid-1": testBirthdayTitle, "vid-2": testBirthdayTitle, "vid-3": testBirthdayTitle, "vid-free": freeChat},
			want:      []string{},
		},
		{
			name: "three unclassified events leave the full cap",
			sessions: []BirthdayStreamSession{
				session("vid-n4", testBirthdayTitle, 4),
				session("vid-n1", testBirthdayTitle, 1),
				session("vid-n3", testBirthdayTitle, 3),
				session("vid-n2", testBirthdayTitle, 2),
			},
			published: map[string]string{"vid-f1": freeChat, "vid-f2": freeChat, "vid-f3": freeChat},
			want:      []string{"vid-n1", "vid-n2", "vid-n3"},
		},
		{
			name:      "unclassified event stays excluded after birthday rename",
			sessions:  []BirthdayStreamSession{session("vid-legacy", testBirthdayTitle, 1)},
			published: map[string]string{"vid-legacy": freeChat},
			want:      []string{},
		},
		{
			name:      "classified event is re-evaluated after unrelated rename",
			sessions:  []BirthdayStreamSession{session("vid-known", freeChat, 1)},
			published: map[string]string{"vid-known": testBirthdayTitle},
			want:      []string{"vid-known"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			selected := selectBirthdayStreamSessionsWithinDailyCap(
				testMemberIDA,
				testBirthdayDate,
				tt.sessions,
				testPublishedBirthdayStreams(tt.published),
			)

			got := make([]string, 0, len(selected))
			for _, s := range selected {
				got = append(got, s.VideoID)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBirthdayStreamRunOnceCapAlreadyPublishedThree(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	prefix := "celebration:birthday_stream:member-101:2026-07-10:"
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-new",
			ChannelID:      testChannelA,
			Title:          testBirthdayTitle,
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC)),
		}},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{
			"vid-1": testBirthdayTitle,
			"vid-2": testBirthdayTitle,
			"vid-3": testBirthdayTitle,
		}),
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Equal(t, []string{prefix}, store.listCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceCapRepublishesKnownAndAddsRemainingDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	sameStart := new(time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC))
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{
			{VideoID: "vid-d", ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming, ScheduledStart: sameStart},
			{VideoID: "vid-a", ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming, ScheduledStart: sameStart},
			{VideoID: "vid-c", ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming, ScheduledStart: sameStart},
			{VideoID: "vid-b", ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming, ScheduledStart: sameStart},
		},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{"vid-a": testBirthdayTitle}),
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 3)
	assert.Equal(t, "vid-a", envelopes[0].Celebration.VideoID)
	assert.Equal(t, "vid-b", envelopes[1].Celebration.VideoID)
	assert.Equal(t, "vid-c", envelopes[2].Celebration.VideoID)
}

func TestBirthdayStreamRunOnceScopesRoomsToMatchingSentBirthday(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {
			{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"},
			{ID: testMemberIDB, ChannelID: testChannelB, Name: "B"},
		},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{
			{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming},
			{VideoID: "vid-b", ChannelID: testChannelB, Title: testBirthdayTitle, Status: testStatusUpcoming},
		},
		sentRoomsByEventKey: map[string][]string{
			birthdayGreetingEventKey(testMemberIDA, testBirthdayDate): {"room-a"},
			birthdayGreetingEventKey(testMemberIDB, testBirthdayDate): {"room-b"},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, nil, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 2)
	assert.Equal(t, testChannelA, envelopes[0].Celebration.ChannelID)
	assert.Equal(t, "room-a", envelopes[0].Notification.RoomID)
	assert.Equal(t, testChannelB, envelopes[1].Celebration.ChannelID)
	assert.Equal(t, "room-b", envelopes[1].Notification.RoomID)
}

func TestBirthdayStreamRunOnceAudienceLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	lookupErr := errors.New("audience unavailable")
	store := &birthdayStreamTestStore{
		sessions:     []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		sentRoomsErr: lookupErr,
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-all"}, now)

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	require.ErrorIs(t, err, lookupErr)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOncePublishedEventLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	lookupErr := errors.New("published event unavailable")
	store := &birthdayStreamTestStore{
		sessions:           []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		publishedEventsErr: lookupErr,
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	require.ErrorIs(t, err, lookupErr)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceGreetingVerificationFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	verifyErr := errors.New("greeting payload resolves to another key")
	store := &birthdayStreamTestStore{
		sessions:                      []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		publishedCelebrationEventsErr: verifyErr,
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	err := runner.RunOnce(t.Context())

	require.ErrorIs(t, err, verifyErr)
	require.Equal(t, [][]string{{birthdayGreetingEventKey(testMemberIDA, testBirthdayDate)}}, store.publishedCelebrationEventCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceEmptySentAudienceNoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, nil, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Equal(t, [][]string{{birthdayGreetingEventKey(testMemberIDA, testBirthdayDate)}}, store.sentRoomsEventKeyCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceRepublishesKnownEventForLateSentRoom(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	prefix := birthdayStreamEventKeyPrefix(testMemberIDA, testBirthdayDate)
	greetingKey := birthdayGreetingEventKey(testMemberIDA, testBirthdayDate)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: "original birthday title", Status: testStatusUpcoming}},
		sentRoomsByEventKey: map[string][]string{
			greetingKey: {testRoom1},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, nil, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	canonicalEnvelope := publisher.batches[0][0]

	canonicalEnvelope.Notification.RoomID = ""
	canonicalEnvelope.Notification.Users = nil

	store.publishedEvents = map[string]domain.AlarmQueueEnvelope{
		birthdayStreamEventKey(testMemberIDA, testBirthdayDate, testVideoA): canonicalEnvelope,
	}
	store.sentRoomsByEventKey[greetingKey] = []string{testRoom1, testRoom2}
	store.sessions[0].Title = "changed upstream title"

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, publisher.batches, 2)
	require.Len(t, publisher.batches[0], 1)
	require.Len(t, publisher.batches[1], 2)
	assert.Equal(t, testRoom1, publisher.batches[1][0].Notification.RoomID)
	assert.Equal(t, testRoom2, publisher.batches[1][1].Notification.RoomID)
	assert.Equal(t, "original birthday title", publisher.batches[1][0].Celebration.StreamTitle)
	assert.Equal(t, "original birthday title", publisher.batches[1][1].Celebration.StreamTitle)
	assert.Equal(t, []string{prefix, prefix}, store.listCalls)
}

// 기발행 생일 방송의 재평가는 이미 sent로 받은 방을 다시 envelope으로 만들지 않고, 늦게 축하받은 방만 보낸다.
func TestBirthdayStreamRunOnceRepublishSkipsRoomsAlreadySentStream(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	greetingKey := birthdayGreetingEventKey(testMemberIDA, testBirthdayDate)
	streamKey := birthdayStreamEventKey(testMemberIDA, testBirthdayDate, testVideoA)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	store := &birthdayStreamTestStore{
		sessions:        []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{testVideoA: testBirthdayTitle}),
		sentRoomsByEventKey: map[string][]string{
			greetingKey: {testRoom1, testRoom2},
			streamKey:   {testRoom1},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, nil, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Equal(t, [][]string{{greetingKey}, {streamKey}}, store.sentRoomsEventKeyCalls)

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	assert.Equal(t, testRoom2, envelopes[0].Notification.RoomID)
	assert.Equal(t, testVideoA, envelopes[0].Celebration.VideoID)
}

// 모든 audience 방이 이미 생일 방송을 받았으면 발행 tx를 열지 않는다.
func TestBirthdayStreamRunOnceRepublishAllRoomsSentSkipsPublish(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	store := &birthdayStreamTestStore{
		sessions:        []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{testVideoA: testBirthdayTitle}),
		sentRoomsByEventKey: map[string][]string{
			birthdayGreetingEventKey(testMemberIDA, testBirthdayDate):           {testRoom1, testRoom2},
			birthdayStreamEventKey(testMemberIDA, testBirthdayDate, testVideoA): {testRoom2, testRoom1},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, nil, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, publisher.batches)
}

// 기발행 생일 방송의 sent 방 조회가 실패하면 audience 전체로 재발행하지 않고 오류로 끝낸다.
func TestBirthdayStreamRunOnceSentStreamRoomLookupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
	}}
	lookupErr := errors.New("stream ledger unavailable")
	store := &birthdayStreamFailingStreamLookupStore{
		sessions:        []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{testVideoA: testBirthdayTitle}),
		streamErr:       lookupErr,
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, &store.birthdayStreamTestStore, publisher, []string{testRoom1}, now)

	runner.sessions = store

	err := runner.RunOnce(t.Context())

	require.ErrorIs(t, err, lookupErr)
	assert.Empty(t, publisher.batches)
}

// birthdayStreamFailingStreamLookupStore는 생일 방송 event_key 조회만 실패시킨다.
type birthdayStreamFailingStreamLookupStore struct {
	birthdayStreamTestStore

	streamErr error
}

func (s *birthdayStreamFailingStreamLookupStore) FindSentRoomsByEventKeys(ctx context.Context, eventKeys []string) (map[string][]string, error) {
	if len(eventKeys) > 0 && eventKeys[0] != birthdayGreetingEventKey(testMemberIDA, testBirthdayDate) {
		return nil, s.streamErr
	}

	return s.birthdayStreamTestStore.FindSentRoomsByEventKeys(ctx, eventKeys)
}

// 모든 envelope이 dedupe로 끝난 실행은 운영 INFO에 남기지 않고, 실제로 delivery를 추가한 실행만 INFO로 남긴다.
func TestBirthdayStreamRunOncePublishLogLevelFollowsInsertedRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result dispatchoutbox.PublishBatchResult
		want   slog.Level
	}{
		{name: "all deduped", result: dispatchoutbox.PublishBatchResult{DuplicateEvents: 1}, want: slog.LevelDebug},
		{name: "late room delivery", result: dispatchoutbox.PublishBatchResult{InsertedDeliveries: 1, DuplicateEvents: 1}, want: slog.LevelInfo},
		{name: "new event", result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1}, want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
			memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
				{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: "A"}},
			}}
			store := &birthdayStreamTestStore{
				sessions: []BirthdayStreamSession{{VideoID: testVideoA, ChannelID: testChannelA, Title: testBirthdayTitle, Status: testStatusUpcoming}},
			}
			publisher := &birthdayStreamTestPublisher{result: tt.result}
			runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

			var logs bytes.Buffer

			runner.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

			require.NoError(t, runner.RunOnce(t.Context()))

			var record struct {
				Level string `json:"level"`
				Msg   string `json:"msg"`
			}

			require.NoError(t, json.Unmarshal(logs.Bytes(), &record))
			assert.Equal(t, "Birthday stream runner published", record.Msg)
			assert.Equal(t, tt.want.String(), record.Level)
		})
	}
}

func TestBirthdayStreamRunOnceSkipsNonBirthdayTitles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 13, 0, 27, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{10, 13}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{
			{VideoID: "vid-dreams", ChannelID: testChannelA, Title: "【hololive Dreams】 DON'T FORGET TO LOGIN TODAY!! #holodori", Status: testStatusUpcoming},
			{VideoID: "vid-vlog", ChannelID: testChannelA, Title: "【SARUSHIMA】 Jalan-jalan di pulau Sarushima?! 【VLOG】", Status: testStatusUpcoming},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, store.sentRoomsEventKeyCalls)
	assert.Empty(t, publisher.batches)
}

// 저장 제목이 생일과 무관한 기발행 event는 상한을 차지하지 않고 다시 발행되지도 않는다.
func TestBirthdayStreamRunOnceUnclassifiedPublishedEventsDoNotConsumeCap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	dreamsTitle := "【hololive Dreams】 DON'T FORGET TO LOGIN TODAY!!"
	vlogTitle := "【SARUSHIMA】 Jalan-jalan di pulau Sarushima?! 【VLOG】"
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ID: testMemberIDA, ChannelID: testChannelA, Name: testMemberName}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{
			{VideoID: "vid-countdown", ChannelID: testChannelA, Title: "【BIRTHDAY COUNTDOWN】", Status: testStatusLive, StartedAt: new(time.Date(2026, time.July, 9, 15, 30, 0, 0, time.UTC))},
			{VideoID: "vid-dreams", ChannelID: testChannelA, Title: dreamsTitle, Status: testStatusUpcoming, ScheduledStart: new(time.Date(2026, time.July, 10, 2, 0, 0, 0, time.UTC))},
			{VideoID: "vid-vlog", ChannelID: testChannelA, Title: vlogTitle, Status: testStatusUpcoming, ScheduledStart: new(time.Date(2026, time.July, 10, 3, 0, 0, 0, time.UTC))},
			{VideoID: "vid-main", ChannelID: testChannelA, Title: "【3D LIVE】Happy Birthday to me!", Status: testStatusUpcoming, ScheduledStart: new(time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC))},
		},
		publishedEvents: testPublishedBirthdayStreams(map[string]string{
			"vid-countdown": "【BIRTHDAY COUNTDOWN】",
			"vid-dreams":    dreamsTitle,
			"vid-vlog":      vlogTitle,
		}),
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 2)
	assert.Equal(t, "vid-countdown", envelopes[0].Celebration.VideoID)
	assert.Equal(t, "vid-main", envelopes[1].Celebration.VideoID)
	assert.Equal(t, "【3D LIVE】Happy Birthday to me!", envelopes[1].Celebration.StreamTitle)
}

func TestBirthdayStreamRunOnceEmptyChannelIDSkipped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {
			{ChannelID: "", Name: "Chzzk Only"},
			{ID: testMemberIDA, ChannelID: testChannelA, Name: "YouTube"},
		},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-1",
			ChannelID:      testChannelA,
			Title:          testBirthdayTitle,
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, 10, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, store.sessionCalls, 1)
	assert.Equal(t, []string{testChannelA}, store.sessionCalls[0].channelIDs)

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	assert.Equal(t, testChannelA, envelopes[0].Celebration.ChannelID)
}

func TestBirthdayStreamRunOnceAllMembersWithoutChannelNoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: " ", Name: "Chzzk Only"}},
	}}
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, store.sessionCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceEnvelopeFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{
			ID:              testMemberIDA,
			ChannelID:       testChannelA,
			ShortKoreanName: "후부키",
			NameKo:          "시라카미 후부키",
			Name:            "Shirakami Fubuki",
			Photo:           "https://example.com/a.jpg",
		}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []BirthdayStreamSession{{
			VideoID:        "vid-1",
			ChannelID:      testChannelA,
			Title:          "생일 방송",
			Status:         testStatusUpcoming,
			ScheduledStart: new(time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{testRoom1, testRoom2}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 2)
	assert.Equal(t, testRoom1, envelopes[0].Notification.RoomID)
	assert.Equal(t, testRoom2, envelopes[1].Notification.RoomID)

	for _, env := range envelopes {
		assert.Equal(t, domain.AlarmTypeBirthday, env.Notification.AlarmType)
		assert.Equal(t, domain.AlarmDispatchSourceKindCelebration, env.SourceKind)
		require.NotNil(t, env.Notification.Channel)
		assert.Equal(t, testChannelA, env.Notification.Channel.ID)

		payload := env.Celebration
		require.NotNil(t, payload)
		assert.Equal(t, domain.CelebrationKindBirthdayStream, payload.Kind)
		assert.Equal(t, "후부키", payload.MemberName)
		assert.Equal(t, testChannelA, payload.ChannelID)
		assert.Equal(t, "https://example.com/a.jpg", payload.Photo)
		assert.Equal(t, testBirthdayDate, payload.Date)
		assert.Equal(t, "vid-1", payload.VideoID)
		assert.Equal(t, "생일 방송", payload.StreamTitle)
		assert.Equal(t, "https://youtube.com/watch?v=vid-1", payload.StreamURL)
		assert.Equal(t, "21:00", payload.ScheduledStartKST)
		assert.Equal(t, "birthday_stream:member-101:2026-07-10:vid-1", payload.Identity())
		require.NoError(t, env.ValidateCanonicalDispatch())
	}
}

func TestBirthdayStreamEventKeyPrefixMatchesEventKey(t *testing.T) {
	t.Parallel()

	prefix := birthdayStreamEventKeyPrefix(testMemberIDA, testBirthdayDate)
	assert.Equal(t, "celebration:birthday_stream:member-101:2026-07-10:", prefix)
	assert.Equal(t, prefix+"vid-1", birthdayStreamEventKey(testMemberIDA, testBirthdayDate, "vid-1"))
	assert.Equal(t, "celebration:birthday:member-101:2026-07-10", birthdayGreetingEventKey(testMemberIDA, testBirthdayDate))
}

func TestBirthdayStreamRunnerStartStopsOnContextEnd(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(&birthdayStreamTestMemberRepo{}, &birthdayStreamTestStore{}, publisher, []string{testRoom1}, now)

	var sleepCalls []time.Duration

	runner.sleep = func(_ context.Context, d time.Duration) bool {
		sleepCalls = append(sleepCalls, d)
		return len(sleepCalls) < 2
	}

	require.NoError(t, runner.Start(t.Context()))
	assert.Equal(t, []time.Duration{30 * time.Minute, 30 * time.Minute}, sleepCalls)
}

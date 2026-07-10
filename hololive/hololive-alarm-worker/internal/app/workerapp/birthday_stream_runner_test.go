package workerapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKST = time.FixedZone("KST", 9*60*60)

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
	sessions         []birthdayStreamSession
	sessionsErr      error
	sessionsErrQueue []error
	sessionCalls     []birthdayStreamSessionQuery
	publishedKeys    map[string][]string
	listCalls        []string
}

func (s *birthdayStreamTestStore) FindBirthdaySessions(
	_ context.Context,
	channelIDs []string,
	windowStartUTC, windowEndUTC, seenSince time.Time,
) ([]birthdayStreamSession, error) {
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

func (s *birthdayStreamTestStore) ListPublishedEventKeys(_ context.Context, keyPrefix string) ([]string, error) {
	s.listCalls = append(s.listCalls, keyPrefix)
	return s.publishedKeys[keyPrefix], nil
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
) *birthdayStreamRunner {
	return &birthdayStreamRunner{
		memberRepo:       memberRepo,
		alarmRepo:        &celebrationTestAlarmRepo{rooms: rooms},
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
			name: "midday tick evaluates single date",
			now:  time.Date(2026, 7, 10, 12, 0, 0, 0, testKST),
			want: []string{"2026-07-10"},
		},
		{
			name: "post-midnight tick also evaluates yesterday",
			now:  time.Date(2026, 7, 11, 0, 5, 0, 0, testKST),
			want: []string{"2026-07-10", "2026-07-11"},
		},
		{
			name: "utc input inside midnight window evaluates both kst days",
			now:  time.Date(2026, 7, 10, 15, 20, 0, 0, time.UTC),
			want: []string{"2026-07-10", "2026-07-11"},
		},
		{
			name: "utc input past midnight window evaluates single kst day",
			now:  time.Date(2026, 7, 10, 15, 40, 0, 0, time.UTC),
			want: []string{"2026-07-11"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dates := birthdayStreamEvaluationDates(tt.now, 30*time.Minute)
			got := make([]string, 0, len(dates))
			for _, d := range dates {
				got = append(got, d.Format("2006-01-02"))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBirthdayStreamRunOnceWindowBoundsAndFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", Name: "Test"}},
	}}
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)
	runner.sessionFreshness = 45 * time.Minute

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, store.sessionCalls, 1)
	call := store.sessionCalls[0]
	assert.Equal(t, []string{"UC_a"}, call.channelIDs)
	assert.True(t, call.windowStart.Equal(time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC)), "windowStart = %v", call.windowStart)
	assert.True(t, call.windowEnd.Equal(time.Date(2026, 7, 10, 15, 0, 0, 0, time.UTC)), "windowEnd = %v", call.windowEnd)
	assert.True(t, call.seenSince.Equal(now.Add(-45*time.Minute)), "seenSince = %v", call.seenSince)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceMidnightCatchesLateFrameWithYesterdayDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 0, 5, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", Name: "Test"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{{
			VideoID:        "vid-late",
			ChannelID:      "UC_a",
			Title:          "late frame",
			Status:         "UPCOMING",
			ScheduledStart: new(time.Date(2026, 7, 10, 14, 30, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Equal(t, [][2]int{{7, 10}, {7, 11}}, memberRepo.calls)
	require.Len(t, store.sessionCalls, 1)
	assert.True(t, store.sessionCalls[0].windowStart.Equal(time.Date(2026, 7, 9, 15, 0, 0, 0, time.UTC)))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Celebration)
	assert.Equal(t, "2026-07-10", envelopes[0].Celebration.Date)
	assert.Equal(t, "vid-late", envelopes[0].Celebration.VideoID)
}

func TestBirthdayStreamRunOnceYesterdayFailureStillEvaluatesToday(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 11, 0, 5, 0, 0, testKST)
	yesterdayErr := errors.New("transient db blip")
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", Name: "Yesterday"}},
		{7, 11}: {{ChannelID: "UC_b", Name: "Today"}},
	}}
	store := &birthdayStreamTestStore{
		sessionsErrQueue: []error{yesterdayErr},
		sessions: []birthdayStreamSession{{
			VideoID:        "vid-today",
			ChannelID:      "UC_b",
			Title:          "today stream",
			Status:         "UPCOMING",
			ScheduledStart: new(time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	err := runner.RunOnce(t.Context())

	require.Error(t, err)
	assert.ErrorIs(t, err, yesterdayErr)
	assert.Equal(t, [][2]int{{7, 10}, {7, 11}}, memberRepo.calls)
	require.Len(t, store.sessionCalls, 2)
	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	require.NotNil(t, envelopes[0].Celebration)
	assert.Equal(t, "2026-07-11", envelopes[0].Celebration.Date)
	assert.Equal(t, "vid-today", envelopes[0].Celebration.VideoID)
}

func TestBirthdayStreamRunOnceNoBirthdayNoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(&birthdayStreamTestMemberRepo{}, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, store.sessionCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceLiveSessionIncluded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 21, 30, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", ShortKoreanName: "후부키"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{{
			VideoID:   "vid-live",
			ChannelID: "UC_a",
			Title:     "guerrilla live",
			Status:    "LIVE",
			StartedAt: new(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	payload := envelopes[0].Celebration
	require.NotNil(t, payload)
	assert.Equal(t, "vid-live", payload.VideoID)
	assert.Equal(t, "https://youtube.com/watch?v=vid-live", payload.StreamURL)
	assert.Equal(t, "21:00", payload.ScheduledStartKST)
}

func TestBirthdayStreamRunOnceCapAlreadyPublishedThree(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	prefix := "celebration:birthday_stream:UC_a:2026-07-10:"
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", Name: "Test"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{{
			VideoID:        "vid-new",
			ChannelID:      "UC_a",
			Status:         "UPCOMING",
			ScheduledStart: new(time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)),
		}},
		publishedKeys: map[string][]string{
			prefix: {prefix + "vid-1", prefix + "vid-2", prefix + "vid-3"},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Equal(t, []string{prefix}, store.listCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceCapPublishesRemainingDeterministically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	prefix := "celebration:birthday_stream:UC_a:2026-07-10:"
	sameStart := new(time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC))
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: "UC_a", Name: "Test"}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{
			{VideoID: "vid-d", ChannelID: "UC_a", Status: "UPCOMING", ScheduledStart: sameStart},
			{VideoID: "vid-a", ChannelID: "UC_a", Status: "UPCOMING", ScheduledStart: sameStart},
			{VideoID: "vid-c", ChannelID: "UC_a", Status: "UPCOMING", ScheduledStart: sameStart},
			{VideoID: "vid-b", ChannelID: "UC_a", Status: "UPCOMING", ScheduledStart: sameStart},
		},
		publishedKeys: map[string][]string{
			prefix: {prefix + "vid-a"},
		},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 2)
	assert.Equal(t, "vid-b", envelopes[0].Celebration.VideoID)
	assert.Equal(t, "vid-c", envelopes[1].Celebration.VideoID)
}

func TestBirthdayStreamRunOnceEmptyChannelIDSkipped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {
			{ChannelID: "", Name: "Chzzk Only"},
			{ChannelID: "UC_a", Name: "YouTube"},
		},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{{
			VideoID:        "vid-1",
			ChannelID:      "UC_a",
			Status:         "UPCOMING",
			ScheduledStart: new(time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	require.Len(t, store.sessionCalls, 1)
	assert.Equal(t, []string{"UC_a"}, store.sessionCalls[0].channelIDs)
	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 1)
	assert.Equal(t, "UC_a", envelopes[0].Celebration.ChannelID)
}

func TestBirthdayStreamRunOnceAllMembersWithoutChannelNoop(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{ChannelID: " ", Name: "Chzzk Only"}},
	}}
	store := &birthdayStreamTestStore{}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	assert.Empty(t, store.sessionCalls)
	assert.Empty(t, publisher.batches)
}

func TestBirthdayStreamRunOnceEnvelopeFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	memberRepo := &birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
		{7, 10}: {{
			ChannelID:       "UC_a",
			ShortKoreanName: "후부키",
			NameKo:          "시라카미 후부키",
			Name:            "Shirakami Fubuki",
			Photo:           "https://example.com/a.jpg",
		}},
	}}
	store := &birthdayStreamTestStore{
		sessions: []birthdayStreamSession{{
			VideoID:        "vid-1",
			ChannelID:      "UC_a",
			Title:          "생일 방송",
			Status:         "UPCOMING",
			ScheduledStart: new(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)),
		}},
	}
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(memberRepo, store, publisher, []string{"room-1", "room-2"}, now)

	require.NoError(t, runner.RunOnce(t.Context()))

	envelopes := publisher.allEnvelopes()
	require.Len(t, envelopes, 2)
	assert.Equal(t, "room-1", envelopes[0].Notification.RoomID)
	assert.Equal(t, "room-2", envelopes[1].Notification.RoomID)
	for _, env := range envelopes {
		assert.Equal(t, domain.AlarmTypeBirthday, env.Notification.AlarmType)
		assert.Equal(t, domain.AlarmDispatchSourceKindCelebration, env.SourceKind)
		require.NotNil(t, env.Notification.Channel)
		assert.Equal(t, "UC_a", env.Notification.Channel.ID)
		payload := env.Celebration
		require.NotNil(t, payload)
		assert.Equal(t, domain.CelebrationKindBirthdayStream, payload.Kind)
		assert.Equal(t, "후부키", payload.MemberName)
		assert.Equal(t, "UC_a", payload.ChannelID)
		assert.Equal(t, "https://example.com/a.jpg", payload.Photo)
		assert.Equal(t, "2026-07-10", payload.Date)
		assert.Equal(t, "vid-1", payload.VideoID)
		assert.Equal(t, "생일 방송", payload.StreamTitle)
		assert.Equal(t, "https://youtube.com/watch?v=vid-1", payload.StreamURL)
		assert.Equal(t, "21:00", payload.ScheduledStartKST)
		assert.Equal(t, "birthday_stream:UC_a:2026-07-10:vid-1", payload.Identity())
		require.NoError(t, env.ValidateCanonicalDispatch())
	}
}

func TestBirthdayStreamEventKeyPrefixMatchesEventKey(t *testing.T) {
	t.Parallel()

	prefix := birthdayStreamEventKeyPrefix("UC_a", "2026-07-10")
	assert.Equal(t, "celebration:birthday_stream:UC_a:2026-07-10:", prefix)
	assert.Equal(t, prefix+"vid-1", birthdayStreamEventKey("UC_a", "2026-07-10", "vid-1"))
}

func TestBirthdayStreamRunnerStartStopsOnContextEnd(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, testKST)
	publisher := &birthdayStreamTestPublisher{}
	runner := newBirthdayStreamTestRunner(&birthdayStreamTestMemberRepo{}, &birthdayStreamTestStore{}, publisher, []string{"room-1"}, now)
	var sleepCalls []time.Duration
	runner.sleep = func(_ context.Context, d time.Duration) bool {
		sleepCalls = append(sleepCalls, d)
		return len(sleepCalls) < 2
	}

	require.NoError(t, runner.Start(t.Context()))
	assert.Equal(t, []time.Duration{30 * time.Minute, 30 * time.Minute}, sleepCalls)
}

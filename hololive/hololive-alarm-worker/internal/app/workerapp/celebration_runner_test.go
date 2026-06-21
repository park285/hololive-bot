package workerapp

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type celebrationTestMemberRepo struct {
	birthday    []*domain.Member
	anniversary []*domain.Member
	birthdayErr error
}

func (r *celebrationTestMemberRepo) FindMembersWithBirthdayOn(context.Context, int, int) ([]*domain.Member, error) {
	return r.birthday, r.birthdayErr
}

func (r *celebrationTestMemberRepo) FindMembersWithAnniversaryOn(context.Context, int, int, int) ([]*domain.Member, error) {
	return r.anniversary, nil
}

type celebrationTestAlarmRepo struct {
	rooms    []string
	roomsErr error
}

func (r *celebrationTestAlarmRepo) GetAllDistinctRoomIDs(context.Context) ([]string, error) {
	return r.rooms, r.roomsErr
}

type celebrationTestPublisher struct {
	envelopes []domain.AlarmQueueEnvelope
	result    dispatchoutbox.PublishBatchResult
	err       error
	onPublish func()
}

func (p *celebrationTestPublisher) PublishDispatchBatch(_ context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	p.envelopes = envelopes
	if p.onPublish != nil {
		p.onPublish()
	}
	return p.result, p.err
}

func TestBuildCelebrationEnvelopesBirthday(t *testing.T) {
	t.Parallel()

	members := []*domain.Member{{
		ChannelID:       "UC_a",
		ShortKoreanName: "후부키",
		Photo:           "https://example.com/a.jpg",
	}}
	rooms := []string{"room-1", "room-2"}

	envelopes := buildCelebrationEnvelopes(members, nil, rooms, "2026-05-26", 2026)

	require.Len(t, envelopes, 2)
	for _, env := range envelopes {
		assert.Equal(t, domain.AlarmTypeBirthday, env.Notification.AlarmType)
		assert.Equal(t, domain.AlarmDispatchSourceKindCelebration, env.SourceKind)
		require.NotNil(t, env.Celebration)
		assert.Equal(t, domain.CelebrationKindBirthday, env.Celebration.Kind)
		assert.Equal(t, "후부키", env.Celebration.MemberName)
		assert.Equal(t, "UC_a", env.Celebration.ChannelID)
		assert.Equal(t, "2026-05-26", env.Celebration.Date)
	}
	assert.Equal(t, "room-1", envelopes[0].Notification.RoomID)
	assert.Equal(t, "room-2", envelopes[1].Notification.RoomID)
}

func TestBuildCelebrationEnvelopesBirthdayOrdinalFromDebut(t *testing.T) {
	t.Parallel()

	birthday := time.Date(2024, 5, 29, 0, 0, 0, 0, time.UTC)
	debut := time.Date(2024, 11, 9, 0, 0, 0, 0, time.UTC)
	members := []*domain.Member{{
		ChannelID:       "UC9LSiN9hXI55svYEBrrK-tw",
		ShortKoreanName: "리오나",
		Birthday:        &birthday,
		DebutDate:       &debut,
	}}

	envelopes := buildCelebrationEnvelopes(members, nil, []string{"room-1"}, "2026-05-29", 2026)

	require.Len(t, envelopes, 1)
	assert.Equal(t, 2, envelopes[0].Celebration.Ordinal)
}

func TestBuildCelebrationEnvelopesBirthdayOrdinalLeapDay(t *testing.T) {
	t.Parallel()

	birthday := time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC)
	debut := time.Date(2021, 8, 23, 0, 0, 0, 0, time.UTC)
	members := []*domain.Member{{
		ChannelID: "UCgmPnx-EEeOrZSg5Tiw7ZRQ",
		Name:      "Hakos Baelz",
		Birthday:  &birthday,
		DebutDate: &debut,
	}}

	envelopes := buildCelebrationEnvelopes(members, nil, []string{"room-1"}, "2028-02-29", 2028)

	require.Len(t, envelopes, 1)
	assert.Equal(t, 2, envelopes[0].Celebration.Ordinal)
}

func TestBuildCelebrationEnvelopesAnniversary(t *testing.T) {
	t.Parallel()

	debut := time.Date(2019, 9, 1, 0, 0, 0, 0, time.UTC)
	members := []*domain.Member{{
		ChannelID: "UC_b",
		Name:      "Tokino Sora",
		NameKo:    "토키노 소라",
		DebutDate: &debut,
	}}
	rooms := []string{"room-1"}

	envelopes := buildCelebrationEnvelopes(nil, members, rooms, "2026-09-01", 2026)

	require.Len(t, envelopes, 1)
	env := envelopes[0]
	assert.Equal(t, domain.AlarmTypeAnniversary, env.Notification.AlarmType)
	assert.Equal(t, domain.CelebrationKindAnniversary, env.Celebration.Kind)
	assert.Equal(t, "토키노 소라", env.Celebration.MemberName)
	assert.Equal(t, 7, env.Celebration.Years)
	assert.Equal(t, "2026-09-01", env.Celebration.Date)
}

func TestBuildCelebrationEnvelopesNilDebutDateSkipped(t *testing.T) {
	t.Parallel()

	members := []*domain.Member{{ChannelID: "UC_c", Name: "No Debut"}}
	envelopes := buildCelebrationEnvelopes(nil, members, []string{"room-1"}, "2026-01-01", 2026)
	assert.Empty(t, envelopes)
}

func TestBuildCelebrationEnvelopesZeroYearsSkipped(t *testing.T) {
	t.Parallel()

	debut := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	members := []*domain.Member{{ChannelID: "UC_d", Name: "Same Year", DebutDate: &debut}}
	envelopes := buildCelebrationEnvelopes(nil, members, []string{"room-1"}, "2026-01-01", 2026)
	assert.Empty(t, envelopes)
}

func TestResolveCelebrationMemberName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "후부키", resolveCelebrationMemberName(&domain.Member{ShortKoreanName: "후부키", NameKo: "시라카미 후부키", Name: "Shirakami Fubuki"}))
	assert.Equal(t, "시라카미 후부키", resolveCelebrationMemberName(&domain.Member{NameKo: "시라카미 후부키", Name: "Shirakami Fubuki"}))
	assert.Equal(t, "Shirakami Fubuki", resolveCelebrationMemberName(&domain.Member{Name: "Shirakami Fubuki"}))
}

func TestCelebrationRunnerRunOnceNoMembers(t *testing.T) {
	publisher := &celebrationTestPublisher{}
	runner := &celebrationRunner{
		memberRepo:   &celebrationTestMemberRepo{},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: -1,
	}

	err := runner.RunOnce(t.Context())

	require.NoError(t, err)
	assert.Nil(t, publisher.envelopes)
}

func TestCelebrationRunnerRunOnceNoRooms(t *testing.T) {
	birthday := time.Date(2000, 5, 26, 0, 0, 0, 0, time.UTC)
	publisher := &celebrationTestPublisher{}
	runner := &celebrationRunner{
		memberRepo:   &celebrationTestMemberRepo{birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}}},
		alarmRepo:    &celebrationTestAlarmRepo{},
		publisher:    publisher,
		checkHourKST: -1,
	}

	err := runner.RunOnce(t.Context())

	require.NoError(t, err)
	assert.Nil(t, publisher.envelopes)
}

func TestCelebrationRunnerRunOncePublishes(t *testing.T) {
	birthday := time.Date(2000, 5, 26, 0, 0, 0, 0, time.UTC)
	publisher := &celebrationTestPublisher{result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 2}}
	runner := &celebrationRunner{
		memberRepo:   &celebrationTestMemberRepo{birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}}},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1", "room-2"}},
		publisher:    publisher,
		checkHourKST: -1,
	}

	err := runner.RunOnce(t.Context())

	require.NoError(t, err)
	require.Len(t, publisher.envelopes, 2)
}

func TestNextCelebrationRunAtFromLatePreviousDay(t *testing.T) {
	t.Parallel()

	kst := time.FixedZone("KST", 9*60*60)
	now := time.Date(2026, time.May, 25, 23, 50, 0, 0, kst)

	got := nextCelebrationRunAt(now, 0)
	want := time.Date(2026, time.May, 26, 0, 0, 0, 0, kst)

	assert.Equal(t, want, got)
}

func TestCelebrationSchedule_SameDayRestart_7f8d8a9e(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	current := time.Date(2026, time.May, 26, 0, 1, 0, 0, kst)
	var sleepCalls []time.Duration

	ctx, cancel := context.WithCancel(t.Context())
	publisher := &celebrationTestPublisher{
		result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1},
		onPublish: func() {
			cancel()
		},
	}
	birthday := time.Date(2000, time.May, 26, 0, 0, 0, 0, time.UTC)
	runner := &celebrationRunner{
		memberRepo: &celebrationTestMemberRepo{
			birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}},
		},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: 0,
		now: func() time.Time {
			return current
		},
		sleep: func(ctx context.Context, d time.Duration) bool {
			sleepCalls = append(sleepCalls, d)
			if ctx.Err() != nil {
				return false
			}
			current = current.Add(d)
			return true
		},
	}

	err := runner.Start(ctx)

	require.NoError(t, err)
	require.NotEmpty(t, sleepCalls)
	assert.LessOrEqual(t, sleepCalls[0], time.Duration(0), "설정 hour 직후 재시작은 당일 즉시 실행해야 한다")
	require.Len(t, publisher.envelopes, 1)
	require.NotNil(t, publisher.envelopes[0].Celebration)
	assert.Equal(t, "2026-05-26", publisher.envelopes[0].Celebration.Date)
}

func TestCelebrationSchedule_RestartOutsideGraceWaitsNextDay_7f8d8a9e(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	current := time.Date(2026, time.May, 26, 2, 30, 0, 0, kst)
	var sleepCalls []time.Duration

	ctx, cancel := context.WithCancel(t.Context())
	publisher := &celebrationTestPublisher{
		result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1},
		onPublish: func() {
			cancel()
		},
	}
	birthday := time.Date(2000, time.May, 27, 0, 0, 0, 0, time.UTC)
	runner := &celebrationRunner{
		memberRepo: &celebrationTestMemberRepo{
			birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}},
		},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: 0,
		now: func() time.Time {
			return current
		},
		sleep: func(ctx context.Context, d time.Duration) bool {
			sleepCalls = append(sleepCalls, d)
			if ctx.Err() != nil {
				return false
			}
			current = current.Add(d)
			return true
		},
	}

	err := runner.Start(ctx)

	require.NoError(t, err)
	require.NotEmpty(t, sleepCalls)
	assert.Equal(t, 21*time.Hour+30*time.Minute, sleepCalls[0], "grace window 밖 재시작은 다음 날 00:00까지 대기해야 한다")
}

func TestCelebrationRunnerStartPublishesAtScheduledMidnight(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	current := time.Date(2026, time.May, 25, 23, 50, 0, 0, kst)
	var publishedAt time.Time
	var sleepCalls []time.Duration

	ctx, cancel := context.WithCancel(t.Context())
	publisher := &celebrationTestPublisher{
		result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1},
		onPublish: func() {
			publishedAt = current
			cancel()
		},
	}
	birthday := time.Date(2000, time.May, 26, 0, 0, 0, 0, time.UTC)
	runner := &celebrationRunner{
		memberRepo: &celebrationTestMemberRepo{
			birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}},
		},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: 0,
		now: func() time.Time {
			return current
		},
		sleep: func(ctx context.Context, d time.Duration) bool {
			sleepCalls = append(sleepCalls, d)
			if ctx.Err() != nil {
				return false
			}
			current = current.Add(d)
			return true
		},
	}

	err := runner.Start(ctx)

	require.NoError(t, err)
	require.NotEmpty(t, sleepCalls)
	assert.Equal(t, 10*time.Minute, sleepCalls[0])
	assert.Equal(t, time.Date(2026, time.May, 26, 0, 0, 0, 0, kst), publishedAt)
	require.Len(t, publisher.envelopes, 1)
	require.NotNil(t, publisher.envelopes[0].Celebration)
	assert.Equal(t, "2026-05-26", publisher.envelopes[0].Celebration.Date)
}

func TestCelebrationRunnerStartPublishesScheduledDateWhenWakeIsLate(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	current := time.Date(2026, time.May, 25, 23, 50, 0, 0, kst)
	var sleepCalls []time.Duration

	ctx, cancel := context.WithCancel(t.Context())
	publisher := &celebrationTestPublisher{
		result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1},
		onPublish: func() {
			cancel()
		},
	}
	birthday := time.Date(2000, time.May, 26, 0, 0, 0, 0, time.UTC)
	runner := &celebrationRunner{
		memberRepo: &celebrationTestMemberRepo{
			birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}},
		},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: 0,
		now: func() time.Time {
			return current
		},
		sleep: func(ctx context.Context, d time.Duration) bool {
			if ctx.Err() != nil {
				return false
			}
			sleepCalls = append(sleepCalls, d)
			if len(sleepCalls) > 1 {
				return false
			}
			current = time.Date(2026, time.May, 26, 1, 5, 0, 0, kst)
			return true
		},
	}

	err := runner.Start(ctx)

	require.NoError(t, err)
	require.NotEmpty(t, sleepCalls)
	assert.Equal(t, 10*time.Minute, sleepCalls[0])
	require.Len(t, publisher.envelopes, 1)
	require.NotNil(t, publisher.envelopes[0].Celebration)
	assert.Equal(t, "2026-05-26", publisher.envelopes[0].Celebration.Date)
}

func TestCelebrationRunnerStartSleepsUntilNextDayAfterScheduledRun(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)
	current := time.Date(2026, time.May, 25, 23, 50, 0, 0, kst)
	var publishCount int
	var sleepCalls []time.Duration

	publisher := &celebrationTestPublisher{
		result: dispatchoutbox.PublishBatchResult{InsertedEvents: 1, InsertedDeliveries: 1},
		onPublish: func() {
			publishCount++
		},
	}
	birthday := time.Date(2000, time.May, 26, 0, 0, 0, 0, time.UTC)
	runner := &celebrationRunner{
		memberRepo: &celebrationTestMemberRepo{
			birthday: []*domain.Member{{ChannelID: "UC_a", Name: "Test", Birthday: &birthday}},
		},
		alarmRepo:    &celebrationTestAlarmRepo{rooms: []string{"room-1"}},
		publisher:    publisher,
		checkHourKST: 0,
		now: func() time.Time {
			return current
		},
		sleep: func(_ context.Context, d time.Duration) bool {
			sleepCalls = append(sleepCalls, d)
			if len(sleepCalls) == 2 {
				return false
			}
			current = current.Add(d)
			return true
		},
	}

	err := runner.Start(t.Context())

	require.NoError(t, err)
	require.Len(t, sleepCalls, 2)
	assert.Equal(t, 10*time.Minute, sleepCalls[0])
	assert.Equal(t, 24*time.Hour, sleepCalls[1])
	assert.Equal(t, 1, publishCount)
}

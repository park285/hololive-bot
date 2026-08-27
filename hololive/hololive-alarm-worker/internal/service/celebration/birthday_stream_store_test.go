package celebration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

type birthdayStreamRepositoryPublisher struct {
	repository *dispatchoutbox.PgxRepository
	results    []dispatchoutbox.PublishBatchResult
}

func (p *birthdayStreamRepositoryPublisher) PublishDispatchBatch(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
) (dispatchoutbox.PublishBatchResult, error) {
	result, err := p.repository.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{
		Envelopes: envelopes,
		Status:    dispatchoutbox.StatusPending,
	})
	if err != nil {
		return result, fmt.Errorf("insert birthday stream dispatch batch: %w", err)
	}

	p.results = append(p.results, result)

	return result, nil
}

func TestPgxStoreFindSentRoomsByEventKeysUsesBirthdayDeliveryLedger(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	eventKey := birthdayGreetingEventKey(testChannelA, testBirthdayDate)
	envelopes := []domain.AlarmQueueEnvelope{
		birthdayGreetingTestEnvelope(testChannelA, "room-sent"),
		birthdayGreetingTestEnvelope(testChannelA, "room-retry"),
		birthdayGreetingTestEnvelope(testChannelB, "room-other-member"),
	}
	_, err := repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: envelopes,
		Status:    dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)

	sentAt := time.Date(2026, time.July, 10, 0, 5, 0, 0, time.UTC)

	_, err = pool.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries d
		SET status = 'sent', sent_at = $1
		FROM alarm_dispatch_events e
		WHERE d.event_id = e.id
		  AND e.event_key = $2
		  AND d.room_id = 'room-sent'
	`, sentAt, eventKey)
	require.NoError(t, err)

	store := NewPgxStore(pool)
	roomsByEventKey, err := store.FindSentRoomsByEventKeys(t.Context(), []string{
		eventKey,
		birthdayGreetingEventKey(testChannelB, testBirthdayDate),
		birthdayGreetingEventKey("UC_missing", testBirthdayDate),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"room-sent"}, roomsByEventKey[eventKey])
	assert.NotContains(t, roomsByEventKey[eventKey], "room-retry")
	assert.NotContains(t, roomsByEventKey, birthdayGreetingEventKey(testChannelB, testBirthdayDate))
	assert.NotContains(t, roomsByEventKey, birthdayGreetingEventKey("UC_missing", testBirthdayDate))
}

func TestBirthdayStreamRunnerReusesPublishedPayloadWhenTitleChanges(t *testing.T) {
	t.Parallel()

	pool := dbtest.NewPool(t)
	repository := dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)
	store := NewPgxStore(pool)
	publisher := &birthdayStreamRepositoryPublisher{repository: repository}
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, testKST)

	seedBirthdayStreamTitleDrift(t, pool, repository, now)

	runner := NewBirthdayStreamRunner(
		&birthdayStreamTestMemberRepo{membersByDay: map[[2]int][]*domain.Member{
			{7, 10}: {{ChannelID: testChannelA, Name: testMemberName}},
		}},
		store,
		publisher,
		nil,
		BirthdayStreamRunnerConfig{RunInterval: 30 * time.Minute, SessionFreshness: 30 * time.Minute},
	)

	runner.now = func() time.Time { return now }

	require.NoError(t, runner.RunOnce(t.Context()))
	require.Len(t, publisher.results, 1)
	assert.Equal(t, 0, publisher.results[0].HashConflictEvents)
	assert.Equal(t, 1, publisher.results[0].InsertedDeliveries)

	setBirthdayStreamTitle(t, pool, "changed upstream title")
	markBirthdayGreetingSent(t, pool, now, testRoom2)

	require.NoError(t, runner.RunOnce(t.Context()))
	require.Len(t, publisher.results, 2)
	assert.Equal(t, 0, publisher.results[1].HashConflictEvents)
	assert.Equal(t, 1, publisher.results[1].InsertedDeliveries)
	assert.Equal(t, 1, publisher.results[1].DuplicateDeliveries)

	assertBirthdayStreamCanonicalState(t, pool)
}

func seedBirthdayStreamTitleDrift(
	t *testing.T,
	pool *pgxpool.Pool,
	repository *dispatchoutbox.PgxRepository,
	now time.Time,
) {
	t.Helper()

	scheduledAt := time.Date(2026, time.July, 10, 12, 30, 0, 0, time.UTC)

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_live_sessions (
			video_id, channel_id, status, title, scheduled_start_time, last_seen_at
		) VALUES ($1, $2, 'UPCOMING', $3, $4, $5)
	`, testVideoA, testChannelA, "original title", scheduledAt, now)
	require.NoError(t, err)

	_, err = repository.InsertBatch(t.Context(), dispatchoutbox.PublishBatchInput{
		Envelopes: []domain.AlarmQueueEnvelope{
			birthdayGreetingTestEnvelope(testChannelA, testRoom1),
			birthdayGreetingTestEnvelope(testChannelA, testRoom2),
		},
		Status: dispatchoutbox.StatusPending,
	})
	require.NoError(t, err)

	markBirthdayGreetingSent(t, pool, now, testRoom1)
}

func markBirthdayGreetingSent(t *testing.T, pool *pgxpool.Pool, sentAt time.Time, roomID string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE alarm_dispatch_deliveries d
		SET status = 'sent', sent_at = $1
		FROM alarm_dispatch_events e
		WHERE d.event_id = e.id
		  AND e.event_key = $2
		  AND d.room_id = $3
	`, sentAt, birthdayGreetingEventKey(testChannelA, testBirthdayDate), roomID)
	require.NoError(t, err)
}

func setBirthdayStreamTitle(t *testing.T, pool *pgxpool.Pool, title string) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		UPDATE youtube_live_sessions
		SET title = $1
		WHERE video_id = $2
	`, title, testVideoA)
	require.NoError(t, err)
}

func assertBirthdayStreamCanonicalState(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	streamEventKey := birthdayStreamEventKey(testChannelA, testBirthdayDate, testVideoA)

	var (
		persistedTitle string
		deliveryCount  int
		collisionCount int
	)

	err := pool.QueryRow(t.Context(), `
		SELECT payload #>> '{celebration,stream_title}'
		FROM alarm_dispatch_events
		WHERE event_key = $1
	`, streamEventKey).Scan(&persistedTitle)
	require.NoError(t, err)

	err = pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM alarm_dispatch_deliveries d
		JOIN alarm_dispatch_events e ON e.id = d.event_id
		WHERE e.event_key = $1
	`, streamEventKey).Scan(&deliveryCount)
	require.NoError(t, err)

	err = pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM alarm_dispatch_event_collisions
		WHERE event_key = $1
	`, streamEventKey).Scan(&collisionCount)
	require.NoError(t, err)

	assert.Equal(t, "original title", persistedTitle)
	assert.Equal(t, 2, deliveryCount)
	assert.Zero(t, collisionCount)
}

func birthdayGreetingTestEnvelope(channelID, roomID string) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    roomID,
			Channel:   &domain.Channel{ID: channelID, Name: channelID},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: channelID,
			ChannelID:  channelID,
			Date:       testBirthdayDate,
		},
	}
}

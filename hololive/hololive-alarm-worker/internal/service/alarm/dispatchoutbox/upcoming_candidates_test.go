package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func candidateNotification(room string, start time.Time) *domain.AlarmNotification {
	stream := &domain.Stream{ID: "staged-video", ChannelID: "staged-channel", Title: "Frozen title", Status: domain.StreamStatusUpcoming, StartScheduled: &start}
	return domain.NewAlarmNotification(room, &domain.Channel{ID: stream.ChannelID}, stream, 5, nil, "")
}

func TestUpcomingCandidatesRecoverFrozenPartialPublishAfterRestart(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	first, second := candidateNotification("stage-a", now.Add(5*time.Minute)), candidateNotification("stage-b", now.Add(5*time.Minute))
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{first, second}))

	// 첫 방 commit 뒤 publish 응답 소실: 수용 receipt를 못 받아도 ledger가 증거다.
	repository := NewPgxRepositoryFromPool(pool, nil)
	_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{{Notification: *first, Version: 1}}})
	require.NoError(t, err)

	first.Stream.Title, second.Stream.Title = "Changed title", "Changed title"
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now.Add(time.Second), []*domain.AlarmNotification{first, second}))

	restarted := NewUpcomingCandidates(pool)
	pending, err := restarted.Pending(t.Context(), now.Add(65*time.Second))
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "stage-b", pending[0].Notification.RoomID)
	require.Equal(t, "Frozen title", pending[0].Notification.Stream.Title)
	require.Equal(t, 5, pending[0].Notification.MinutesUntil)

	pending, err = restarted.Pending(t.Context(), now.Add(5*time.Minute))
	require.NoError(t, err)
	require.Empty(t, pending)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id = 'stage-b'").Scan(&outcome))
	require.Equal(t, "expired", outcome)
}

type lostStageResponseDB struct{ *pgxpool.Pool }

func (db lostStageResponseDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := db.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return tag, err
	}

	return tag, errors.New("response lost after commit")
}

func TestUpcomingCandidatesCommitResponseLossAndAtomicCheckpoint(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	notification := candidateNotification("stage-response", now.Add(5*time.Minute))
	err := NewUpcomingCandidates(lostStageResponseDB{pool}).Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{notification})
	require.ErrorContains(t, err, "response lost")

	recovered, err := NewUpcomingCandidates(pool).Pending(t.Context(), now.Add(65*time.Second))
	require.NoError(t, err)
	require.Len(t, recovered, 1)

	var checkpoint time.Time

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT evaluated_at FROM alarm_upcoming_checkpoints WHERE channel_id = 'staged-channel'").Scan(&checkpoint))
	require.True(t, checkpoint.Equal(now))

	// DB constraint 실패는 같은 문장의 checkpoint도 전진시키지 않는다.
	invalid := candidateNotification(strings.Repeat("r", 101), now.Add(5*time.Minute))

	err = NewUpcomingCandidates(pool).Stage(t.Context(), "staged-channel", now.Add(time.Minute), []*domain.AlarmNotification{invalid})
	require.Error(t, err)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT evaluated_at FROM alarm_upcoming_checkpoints WHERE channel_id = 'staged-channel'").Scan(&checkpoint))
	require.True(t, checkpoint.Equal(now))
}

func TestUpcomingCandidatesRejectCollision(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	n := candidateNotification("stage-collision", now.Add(5*time.Minute))
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{n}))

	n.Stream.Title = "Conflicting event"

	repository := NewPgxRepositoryFromPool(pool, nil)
	_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{{Notification: *n, Version: 1}}})
	require.NoError(t, err)

	pending, err := store.Pending(t.Context(), now.Add(time.Second))
	require.NoError(t, err)
	require.Empty(t, pending)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id = 'stage-collision'").Scan(&outcome))
	require.Equal(t, "rejected_collision", outcome)
}

func TestUpcomingCandidatesLargeFanoutAtomicAndBoundedRecovery(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	store := NewUpcomingCandidates(pool)
	notifications := make([]*domain.AlarmNotification, UpcomingCandidateLimit+1)

	for i := range notifications {
		notifications[i] = candidateNotification(fmt.Sprintf("fanout-%04d", i), now.Add(5*time.Minute))
	}

	notifications[len(notifications)-1].RoomID = strings.Repeat("r", 101)
	require.Error(t, store.Stage(t.Context(), "staged-channel", now, notifications))

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_candidates").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_checkpoints").Scan(&count))
	require.Zero(t, count)

	notifications[len(notifications)-1].RoomID = "fanout-last"
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, notifications))

	first, err := store.Pending(t.Context(), now.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, first, UpcomingCandidateLimit)

	second, err := store.Pending(t.Context(), now.Add(2*time.Second))
	require.NoError(t, err)
	require.Len(t, second, UpcomingCandidateLimit)

	seen := make(map[string]bool)

	for _, c := range first {
		seen[c.DedupeKey] = true
	}

	for _, c := range second {
		seen[c.DedupeKey] = true
	}

	require.Len(t, seen, UpcomingCandidateLimit+1)
}

func TestUpcomingCandidatesCleanupPreservesPendingAndCheckpoint(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	store := NewUpcomingCandidates(pool)
	notifications := []*domain.AlarmNotification{candidateNotification("cleanup-done", now.Add(5*time.Minute)), candidateNotification("cleanup-pending", now.Add(5*time.Minute))}
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now.Add(-72*time.Hour), notifications))

	key := BuildDedupeKeyFromEnvelope(&domain.AlarmQueueEnvelope{Notification: *notifications[0]})
	require.NoError(t, store.Finish(t.Context(), key, "subscription_removed", now.Add(-48*time.Hour)))

	deleted, err := store.Cleanup(t.Context(), 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var count int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_candidates WHERE outcome = 'pending'").Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_upcoming_checkpoints").Scan(&count))
	require.Equal(t, 1, count)
}

func TestUpcomingCandidatesDoNotReopenUnknownDeliveryAtExpiry(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	n := candidateNotification("unknown-room", now.Add(5*time.Minute))
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{n}))

	repository := NewPgxRepositoryFromPool(pool, nil)
	_, err := repository.InsertBatch(t.Context(), PublishBatchInput{Envelopes: []domain.AlarmQueueEnvelope{{Notification: *n, Version: 1}}})
	require.NoError(t, err)

	claimed, err := repository.ClaimDue(t.Context(), "unknown-worker", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, repository.MarkSending(t.Context(), []int64{claimed[0].ID}, "unknown-worker", time.Minute))
	require.NoError(t, repository.Quarantine(t.Context(), []TerminalUpdate{{ID: claimed[0].ID, ErrorCode: "OUTCOME_UNKNOWN", Error: "ambiguous provider result"}}, "unknown-worker"))

	pending, err := store.Pending(t.Context(), now.Add(6*time.Minute))
	require.NoError(t, err)
	require.Empty(t, pending)

	var outcome, status string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT c.outcome,d.status FROM alarm_upcoming_candidates c JOIN alarm_dispatch_deliveries d USING(dedupe_key)").Scan(&outcome, &status))
	require.Equal(t, "rejected_terminal", outcome)
	require.Equal(t, string(StatusQuarantined), status)
}

func TestUpcomingCandidatesHonorNewerCanonicalFactsOnly(t *testing.T) {
	pool := dbtest.NewPool(t)
	now := time.Now().UTC().Truncate(time.Second)
	n := candidateNotification("canonical-room", now.Add(5*time.Minute))
	store := NewUpcomingCandidates(pool)
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{n}))

	_, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_sessions(video_id,channel_id,status,scheduled_start_time,last_seen_at)
		VALUES('staged-video','staged-channel','UPCOMING',$1,$1)`, now.Add(10*time.Minute))
	require.NoError(t, err)

	pending, err := store.Pending(t.Context(), now.Add(time.Second))
	require.NoError(t, err)
	require.Len(t, pending, 1, "stale canonical schedule must not cancel selected snapshot")

	_, err = pool.Exec(t.Context(), "UPDATE youtube_live_sessions SET schedule_observed_at=$1 WHERE video_id='staged-video'", now.Add(time.Second))
	require.NoError(t, err)

	pending, err = store.Pending(t.Context(), now.Add(2*time.Second))
	require.NoError(t, err)
	require.Empty(t, pending)

	var outcome string

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id='canonical-room'").Scan(&outcome))
	require.Equal(t, "schedule_changed", outcome)

	n.RoomID = "ended-room"
	require.NoError(t, store.Stage(t.Context(), "staged-channel", now, []*domain.AlarmNotification{n}))

	_, err = pool.Exec(t.Context(), "UPDATE youtube_live_sessions SET status='ENDED',status_observed_at=$1 WHERE video_id='staged-video'", now.Add(2*time.Second))
	require.NoError(t, err)

	pending, err = store.Pending(t.Context(), now.Add(3*time.Second))
	require.NoError(t, err)
	require.Empty(t, pending)
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT outcome FROM alarm_upcoming_candidates WHERE room_id='ended-room'").Scan(&outcome))
	require.Equal(t, "stream_ended", outcome)
}

package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	eventGradeOwner      = "youtube-collector-c:event-grade-test"
	claimCatalogSQL      = "SELECT job_key, fence_epoch, claimed_at, interrupted FROM public.event_grade_claim_catalog($1, $2, 60000)"
	claimCutoffsSQL      = "SELECT job_key, event_id, chapter_id, fence_epoch, claimed_at FROM public.event_grade_claim_cutoffs($1, $2, 60000, 3)"
	publishSQL           = "SELECT public.event_grade_publish($1, $2, $3, $4, $5, $6::jsonb)"
	releaseSQL           = "SELECT public.event_grade_release($1, $2, $3)"
	observationFlagSQL   = "SELECT kind, (payload->>'quarantine_missing')::boolean FROM public.event_grade_observations WHERE region = $1 AND seq = $2"
	insertObservationSQL = "INSERT INTO public.event_grade_observations(region,seq,schema_version,kind,job_key,scheduled_for,owner_instance,fence_epoch,started_at,observed_at,published_at,payload) VALUES ('jp',99,1,'catalog','x',now(),'x',1,now(),now(),now(),'{}')"
)

type eventGradeClaim struct {
	jobKey    string
	epoch     int64
	claimedAt time.Time
}

func eventGradeSession(t *testing.T, pool *pgxpool.Pool, role string) *pgxpool.Pool {
	t.Helper()

	if _, err := pool.Exec(t.Context(), "GRANT USAGE ON SCHEMA public TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}

	authorize := "SET SESSION AUTHORIZATION " + pgx.Identifier{role}.Sanitize()
	cfg := pool.Config()

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, authorize)
		return err
	}

	session, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(session.Close)

	return session
}

func claimEventGradeCatalog(t *testing.T, session *pgxpool.Pool) (eventGradeClaim, bool) {
	t.Helper()

	var (
		claim       eventGradeClaim
		interrupted bool
	)

	require.NoError(t, session.QueryRow(t.Context(), claimCatalogSQL, "jp", eventGradeOwner).Scan(&claim.jobKey, &claim.epoch, &claim.claimedAt, &interrupted))

	return claim, interrupted
}

func eventGradeCatalogPayload(now time.Time) string {
	starts := now.Add(-time.Hour).Format(time.RFC3339Nano)
	ends := now.Add(time.Hour).Format(time.RFC3339Nano)
	reveals := now.Add(2 * time.Hour).Format(time.RFC3339Nano)

	return `{"chapters":[{"event_id":"ev1","chapter_id":"ch1","event_type":"normal","starts_at":"` + starts + `","ends_at":"` + ends +
		`","event_ends_at":"` + reveals + `","reveals_at":"` + reveals + `"}],"bindings":[{"event_id":"ev1","chapter_id":"ch1"}],"conflicts":[],"quarantine_missing":false}`
}

func TestEventGradeCollectorCPublishesOnlyWithCurrentProof(t *testing.T) {
	pool := NewPool(t)
	collector := eventGradeSession(t, pool, "hololive_collector_c")
	ctx := t.Context()

	claim, interrupted := claimEventGradeCatalog(t, collector)
	require.False(t, interrupted)
	require.Equal(t, int64(1), claim.epoch)

	payload := eventGradeCatalogPayload(claim.claimedAt)

	var seq int64

	require.NoError(t, collector.QueryRow(ctx, publishSQL, claim.jobKey, eventGradeOwner, claim.epoch, claim.claimedAt, claim.claimedAt, payload).Scan(&seq))
	require.Equal(t, int64(1), seq)

	var replayed int64

	require.NoError(t, collector.QueryRow(ctx, publishSQL, claim.jobKey, eventGradeOwner, claim.epoch, claim.claimedAt, claim.claimedAt, payload).Scan(&replayed))
	require.Equal(t, seq, replayed)

	_, err := collector.Exec(ctx, publishSQL, claim.jobKey, eventGradeOwner, claim.epoch, claim.claimedAt, claim.claimedAt, `{"chapters":[],"bindings":[],"conflicts":[],"quarantine_missing":false}`)
	requireSQLState(t, err, "23505")

	var cutoff eventGradeClaim

	var eventID, chapterID string

	require.NoError(t, collector.QueryRow(ctx, claimCutoffsSQL, "jp", eventGradeOwner).Scan(&cutoff.jobKey, &eventID, &chapterID, &cutoff.epoch, &cutoff.claimedAt))
	require.Equal(t, "ch1", chapterID)

	ranks := `{"event_id":"ev1","chapter_id":"ch1","board_kind":"marathon_score","final":false,"ranks":[{"rank":1,"score":"10"}]}`

	_, err = collector.Exec(ctx, publishSQL, cutoff.jobKey, eventGradeOwner, cutoff.epoch-1, cutoff.claimedAt, cutoff.claimedAt, ranks)
	requireSQLState(t, err, "42501")

	_, err = collector.Exec(ctx, publishSQL, cutoff.jobKey, "youtube-collector-c:other", cutoff.epoch, cutoff.claimedAt, cutoff.claimedAt, ranks)
	requireSQLState(t, err, "42501")

	require.NoError(t, collector.QueryRow(ctx, publishSQL, cutoff.jobKey, eventGradeOwner, cutoff.epoch, cutoff.claimedAt, cutoff.claimedAt, ranks).Scan(&seq))
	require.Equal(t, int64(2), seq)

	_, err = collector.Exec(ctx, claimCatalogSQL, "jp", "youtube-collector-a:forged")
	requireSQLState(t, err, "42501")
}

func TestEventGradeInterruptedCatalogQuarantinesMissingBindings(t *testing.T) {
	pool := NewPool(t)
	collector := eventGradeSession(t, pool, "hololive_collector_c")
	ctx := t.Context()

	first, interrupted := claimEventGradeCatalog(t, collector)
	require.False(t, interrupted)

	var released bool

	require.NoError(t, collector.QueryRow(ctx, releaseSQL, first.jobKey, eventGradeOwner, first.epoch).Scan(&released))
	require.True(t, released)

	second, interrupted := claimEventGradeCatalog(t, collector)
	require.True(t, interrupted)

	var seq int64

	require.NoError(t, collector.QueryRow(ctx, publishSQL, second.jobKey, eventGradeOwner, second.epoch, second.claimedAt, second.claimedAt, eventGradeCatalogPayload(second.claimedAt)).Scan(&seq))

	var (
		kind       string
		quarantine bool
	)

	require.NoError(t, pool.QueryRow(ctx, observationFlagSQL, "jp", seq).Scan(&kind, &quarantine))
	require.Equal(t, "catalog", kind)
	require.True(t, quarantine)
}

func TestEventGradeCollectionRejectsOtherDatabaseIdentities(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	other := eventGradeSession(t, pool, "hololive_collector_d")
	_, err := other.Exec(ctx, claimCatalogSQL, "jp", "youtube-collector-d:forged")
	requireSQLState(t, err, "42501")

	_, err = other.Exec(ctx, insertObservationSQL)
	requireSQLState(t, err, "42501")

	worker := eventGradeSession(t, pool, "hololive_alarm_worker")

	_, err = worker.Exec(ctx, insertObservationSQL)
	requireSQLState(t, err, "42501")

	_, err = worker.Exec(ctx, publishSQL, "catalog:jp", eventGradeOwner, 1, time.Now(), time.Now(), "{}")
	requireSQLState(t, err, "42501")

	var offsets int

	require.NoError(t, worker.QueryRow(ctx, "SELECT count(*) FROM public.event_grade_consumer_offsets").Scan(&offsets))
	require.Equal(t, 3, offsets)
}

func TestEventGradeRejectsExpiredProofAfterReclaim(t *testing.T) {
	pool := NewPool(t)
	collector := eventGradeSession(t, pool, "hololive_collector_c")
	ctx := t.Context()

	claim, _ := claimEventGradeCatalog(t, collector)
	_, err := collector.Exec(ctx, publishSQL, claim.jobKey, eventGradeOwner, claim.epoch, claim.claimedAt, claim.claimedAt, eventGradeCatalogPayload(claim.claimedAt))
	require.NoError(t, err)

	const shortClaimSQL = "SELECT job_key, fence_epoch, claimed_at FROM public.event_grade_claim_cutoffs($1, $2, 1000, 1)"

	var stale eventGradeClaim

	require.NoError(t, collector.QueryRow(ctx, shortClaimSQL, "jp", eventGradeOwner).Scan(&stale.jobKey, &stale.epoch, &stale.claimedAt))

	_, err = pool.Exec(ctx, "UPDATE public.event_grade_collection_leases SET lease_expires_at = statement_timestamp() - interval '1 second' WHERE job_key = $1", stale.jobKey)
	require.NoError(t, err)

	const nextOwner = "youtube-collector-c:event-grade-next"

	var current eventGradeClaim

	require.NoError(t, collector.QueryRow(ctx, shortClaimSQL, "jp", nextOwner).Scan(&current.jobKey, &current.epoch, &current.claimedAt))
	require.Equal(t, stale.jobKey, current.jobKey)
	require.Equal(t, stale.epoch+1, current.epoch)

	ranks := `{"event_id":"ev1","chapter_id":"ch1","board_kind":"marathon_score","final":false,"ranks":[{"rank":1,"score":"10"}]}`

	_, err = collector.Exec(ctx, publishSQL, stale.jobKey, eventGradeOwner, stale.epoch, stale.claimedAt, stale.claimedAt, ranks)
	requireSQLState(t, err, "42501")

	_, err = collector.Exec(ctx, publishSQL, current.jobKey, nextOwner, current.epoch, current.claimedAt, current.claimedAt, ranks)
	require.NoError(t, err)
}

package dbtest

import (
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestRetireHolodexProfileMigrationPreservesObservations(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()
	fixture := seedRetiredHolodexProfileMigration(t, pool)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const name = "031_retire_holodex_channel_profile.sql"

	raw, err := fs.ReadFile(os.DirFS(dir), name)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, applyMigrationContent(ctx, pool, name, string(raw)))
	}

	var terminal int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_queue WHERE observation_id IN ($1,$2) AND status='DEAD_LETTER'`, fixture.profile, fixture.pending).Scan(&terminal))
	require.Equal(t, 2, terminal)

	var processedStatus string

	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM source_observation_queue WHERE observation_id=$1`, fixture.processed).Scan(&processedStatus))
	require.Equal(t, "PROCESSED", processedStatus)

	var (
		after, status, code string
		leasesCleared       bool
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(observation)::text || payload.payload::text FROM source_observations observation JOIN source_observation_payloads payload ON payload.id=observation.payload_id WHERE observation.id=$1`, fixture.profile).Scan(&after))
	require.Equal(t, fixture.before, after)

	var headAfter string

	require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(head)::text FROM youtube_channel_profile_heads head WHERE channel_id='preserved'`).Scan(&headAfter))
	require.Equal(t, fixture.headBefore, headAfter)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET status='PENDING',dead_lettered_at=NULL WHERE observation_id=$1`, fixture.profile)
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx, `SELECT status,last_error_code,lease_owner IS NULL AND lease_token IS NULL AND lease_expires_at IS NULL
		FROM source_observation_queue WHERE observation_id=$1`, fixture.profile).Scan(&status, &code, &leasesCleared))
	require.Equal(t, "DEAD_LETTER", status)
	require.Equal(t, "unsupported_contract", code)
	require.True(t, leasesCleared)

	var active int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM source_observation_queue WHERE observation_id IN ($1,$2) AND status='PENDING'`, fixture.photo, fixture.youtubejs).Scan(&active))
	require.Equal(t, 2, active)

	_, err = pool.Exec(ctx, `INSERT INTO source_observations SELECT (jsonb_populate_record(NULL::source_observations,
		to_jsonb(observation)||jsonb_build_object('id',id+1000000,'subject_key','rejected-profile',
		'observation_key',encode(sha256('rejected-profile'::bytea),'hex')))).* FROM source_observations observation WHERE id=$1`, fixture.profile)
	require.ErrorContains(t, err, "contract is retired")
}

type retiredHolodexProfileFixture struct {
	profile, photo, youtubejs, pending, processed int64
	before, headBefore                            string
}

func seedRetiredHolodexProfileMigration(t *testing.T, pool *pgxpool.Pool) retiredHolodexProfileFixture {
	t.Helper()

	ctx := t.Context()
	_, err := pool.Exec(ctx, `ALTER TABLE source_observation_queue DISABLE TRIGGER terminalize_retired_holodex_channel_profile`)
	require.NoError(t, err)

	profile := insertRetentionObservation(t, pool, "channel_profile", time.Now(), "retired-profile")
	pending := insertRetentionObservation(t, pool, "channel_profile", time.Now(), "pending-profile")
	processed := insertRetentionObservation(t, pool, "channel_profile", time.Now(), "processed-profile")
	photo := insertRetentionObservation(t, pool, "channel_photo", time.Now(), "active-photo")
	youtubejs := insertRetentionObservation(t, pool, "channel_profile", time.Now(), "active-profile")

	_, err = pool.Exec(ctx, `UPDATE source_observations SET provider='holodex' WHERE id IN ($1,$2,$3,$4)`, profile, photo, pending, processed)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO source_observation_queue (observation_id) VALUES ($1),($2),($3),($4),($5)`, profile, photo, youtubejs, pending, processed)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET status='PROCESSING', lease_owner='old-worker',
		lease_token=repeat('a',64),lease_expires_at=now()+interval '1 hour' WHERE observation_id=$1`, profile)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_channel_profile_heads(channel_id,description_set,description) VALUES ('preserved',true,'existing profile')`)
	require.NoError(t, err)

	var headBefore string

	require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(head)::text FROM youtube_channel_profile_heads head WHERE channel_id='preserved'`).Scan(&headBefore))

	_, err = pool.Exec(ctx, `ALTER TABLE source_observation_queue ENABLE TRIGGER terminalize_retired_holodex_channel_profile`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE source_observation_queue SET status='PROCESSED',processed_at=now() WHERE observation_id=$1`, processed)
	require.NoError(t, err)

	var before string

	require.NoError(t, pool.QueryRow(ctx, `SELECT row_to_json(observation)::text || payload.payload::text FROM source_observations observation JOIN source_observation_payloads payload ON payload.id=observation.payload_id WHERE observation.id=$1`, profile).Scan(&before))

	return retiredHolodexProfileFixture{profile: profile, photo: photo, youtubejs: youtubejs, pending: pending, processed: processed, before: before, headBefore: headBefore}
}

package dbtest

import (
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetiredProfileTriggersWithObservationRoles(t *testing.T) {
	t.Parallel()

	pool := NewBlankPool(t)
	roles := createObservationGrantRoles(t, pool)
	applyBaselineForRoles(t, pool, roles)

	ctx := t.Context()
	profile := insertRetentionObservation(t, pool, "channel_profile", time.Now(), "legacy-role-profile")
	photo := insertRetentionObservation(t, pool, "channel_photo", time.Now(), "healthy-role-photo")
	_, err := pool.Exec(ctx, `UPDATE source_observations SET provider='holodex' WHERE id IN ($1,$2)`, profile, photo)
	require.NoError(t, err)

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	const name = "031_retire_holodex_channel_profile.sql"

	raw, err := fs.ReadFile(os.DirFS(dir), name)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, name, string(raw)))

	for _, role := range []string{roles.scraper, roles.runtime} {
		tx := payloadRoleTransaction(t, pool, role)

		var status string

		require.NoError(t, tx.QueryRow(ctx, `INSERT INTO source_observation_queue(observation_id) VALUES ($1) RETURNING status`, profile).Scan(&status))
		require.Equal(t, "DEAD_LETTER", status)
		require.NoError(t, tx.QueryRow(ctx, `INSERT INTO source_observation_queue(observation_id) VALUES ($1) RETURNING status`, photo).Scan(&status))
		require.Equal(t, "PENDING", status)

		if role == roles.runtime {
			require.NoError(t, tx.QueryRow(ctx, `UPDATE source_observation_queue SET status='PROCESSING',dead_lettered_at=NULL,
    lease_owner='legacy-worker',lease_token=repeat('a',64),lease_expires_at=now()+interval '1 hour'
    WHERE observation_id=$1 RETURNING status`, profile).Scan(&status))
			require.Equal(t, "DEAD_LETTER", status)
		}

		require.NoError(t, tx.Rollback(ctx))
	}

	tx := payloadRoleTransaction(t, pool, roles.scraper)

	_, err = tx.Exec(ctx, `INSERT INTO source_observations (
		provider,observation_kind,subject_key,observation_key,schema_version,contract_generation,
		scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
		provenance_id,fence_epoch,projection_generation)
		SELECT provider,observation_kind,subject_key,sha256('healthy-copy'::bytea),schema_version,contract_generation,
		scheduled_for,observed_at,scope_sha256,completeness,continuity,payload_id,evidence_sha256,
		provenance_id,fence_epoch,projection_generation FROM source_observations WHERE id=$1`, photo)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	tx = payloadRoleTransaction(t, pool, roles.scraper)
	_, err = tx.Exec(ctx, `INSERT INTO source_observations SELECT (jsonb_populate_record(NULL::source_observations,
		to_jsonb(observation)||jsonb_build_object('id',id+1000000))).* FROM source_observations observation WHERE id=$1`, profile)
	require.ErrorContains(t, err, "contract is retired")
	require.NoError(t, tx.Rollback(ctx))
}

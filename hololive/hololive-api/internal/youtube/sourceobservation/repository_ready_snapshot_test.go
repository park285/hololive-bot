package sourceobservation

import (
	"strings"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func assertReadyDepth(t *testing.T, repo *Repository, kinds []contract.ObservationKind, depth int64) {
	t.Helper()

	snapshot, err := repo.ReadySnapshot(t.Context(), kinds, MaxAttempts)
	require.NoError(t, err)
	require.Equal(t, depth, snapshot.Depth)
}

func TestReadySnapshotUsesSchedulingAndExpiredLeaseThresholds(t *testing.T) {
	for _, processing := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "expired lease"}[processing], func(t *testing.T) {
			pool := dbtest.NewPool(t)
			repo := NewRepository(pool)
			proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
			id := publishOne(t.Context(), t, repo, &proof, "snapshot-age")
			_, err := pool.Exec(t.Context(), `UPDATE source_observations SET received_at = NOW() - INTERVAL '1 day' WHERE id = $1`, id)
			require.NoError(t, err)

			if processing {
				batch, claimErr := repo.ClaimBatch(t.Context(), claimOptions())
				require.NoError(t, claimErr)
				require.Len(t, batch.Claims, 1)
			}

			tx, err := pool.Begin(t.Context())
			require.NoError(t, err)

			defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

			_, err = tx.Exec(t.Context(), `UPDATE source_observation_queue SET available_at = CASE WHEN status = 'PROCESSING' THEN NOW() - INTERVAL '1 day' ELSE NOW() END, lease_expires_at = CASE WHEN status = 'PROCESSING' THEN NOW() ELSE NULL END WHERE observation_id = $1`, id)
			require.NoError(t, err)

			var (
				depth int64
				age   float64
			)

			err = tx.QueryRow(t.Context(), mustSQL("repository_ready_snapshot.sql"), []string{"community_page"}, MaxAttempts).Scan(&depth, &age)
			require.NoError(t, err)
			require.Equal(t, int64(1), depth)
			require.Zero(t, age, "age is the scheduling/lease threshold, not observation receipt")

			claims, err := claimObservations(t.Context(), tx, claimOptions(), strings.Repeat("b", 64))
			require.NoError(t, err)
			require.Len(t, claims, 1, "snapshot and claim include exact lease/available_at equality")
			require.Equal(t, id, claims[0].ObservationID)
		})
	}
}

func TestReadySnapshotReplayCutoffEqualityRemainsClaimable(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindCommunityPage, testChannelID, "community_collect")
	id := publishOne(t.Context(), t, repo, &proof, "epoch-equality")
	activation := activateReplayEpoch(t, repo)
	_, err := pool.Exec(t.Context(), `UPDATE source_observations SET received_at = $2 WHERE id = $1`, id, activation.Epoch.CutoffReceivedAt)
	require.NoError(t, err)
	assertReadyDepth(t, repo, claimOptions().Kinds, 1)

	batch, err := repo.ClaimBatch(t.Context(), claimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, id, batch.Claims[0].ObservationID)
}

func TestReadySnapshotLockedListHeadDoesNotExposeFollower(t *testing.T) {
	pool := dbtest.NewPool(t)
	repo := NewRepository(pool)
	proof := seedPublishLease(t.Context(), t, pool, contract.ProviderYouTubeJS, contract.KindShortsList, testChannelID, "youtubejs_content")
	first := publishShortWindow(t, repo, &proof, "first")

	proof = advanceLease(t.Context(), t, pool, &proof, time.Minute)
	publishShortWindow(t, repo, &proof, "second")

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, pgxdb.Rollback(t.Context(), tx)) }()

	_, err = tx.Exec(t.Context(), `SELECT observation_id FROM source_observation_queue WHERE observation_id = $1 FOR UPDATE`, first)
	require.NoError(t, err)
	// 읽기 전용 snapshot은 순간적 행 잠금이 아니라 durable 선두 후보를 센다.
	assertReadyDepth(t, repo, contentClaimOptions().Kinds, 1)

	batch, err := repo.ClaimBatch(t.Context(), contentClaimOptions())
	require.NoError(t, err)
	require.Empty(t, batch.Claims, "SKIP LOCKED must not let the follower overtake its locked head")
	require.NoError(t, tx.Rollback(t.Context()))

	batch, err = repo.ClaimBatch(t.Context(), contentClaimOptions())
	require.NoError(t, err)
	require.Len(t, batch.Claims, 1)
	require.Equal(t, first, batch.Claims[0].ObservationID)
}

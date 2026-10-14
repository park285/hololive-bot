package sourceobservation

import (
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

func TestApplicationIdempotenceAndCascade(t *testing.T) {
	pool := dbtest.NewPool(t)
	ids := publishProcessedObservations(t.Context(), t, pool, NewRepository(pool), 2)
	insert := `INSERT INTO source_observation_applications
		(observation_id, entity_kind, entity_key, decision)
		VALUES ($1, 'retention_test', 'same-entity', 'APPLIED')
		ON CONFLICT (observation_id, entity_kind, entity_key)
		WHERE observation_id IS NOT NULL DO NOTHING`

	for i, id := range []int64{ids[0], ids[0], ids[1]} {
		tag, err := pool.Exec(t.Context(), insert, id)
		require.NoError(t, err)
		require.EqualValues(t, []int{1, 0, 1}[i], tag.RowsAffected())
	}

	_, err := pool.Exec(t.Context(), insert, nil)
	require.Error(t, err, "감사는 원본 없이 생성할 수 없다")

	_, err = pool.Exec(t.Context(), `DELETE FROM source_observations WHERE id=$1`, ids[0])
	require.NoError(t, err)

	var remaining int

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observation_applications
		WHERE entity_kind='retention_test' AND entity_key='same-entity'`).Scan(&remaining))
	require.Equal(t, 1, remaining)
}

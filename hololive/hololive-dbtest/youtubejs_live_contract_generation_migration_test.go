package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestYoutubejsLiveContractGenerationMigration(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	const query = `SELECT observation_kind, current_schema_version, current_generation, updated_by
		FROM observation_contract_generations
		WHERE provider = 'youtubejs' AND observation_kind IN ('live_snapshot', 'video_live_check')
		ORDER BY observation_kind`

	type generation struct {
		kind      string
		schema    int
		gen       int
		updatedBy string
	}

	load := func() []generation {
		rows, err := pool.Query(ctx, query)
		require.NoError(t, err)

		defer rows.Close()

		var got []generation

		for rows.Next() {
			var g generation

			require.NoError(t, rows.Scan(&g.kind, &g.schema, &g.gen, &g.updatedBy))

			got = append(got, g)
		}

		require.NoError(t, rows.Err())

		return got
	}

	want := []generation{
		{kind: "live_snapshot", schema: 1, gen: 3, updatedBy: "migration-008"},
		{kind: "video_live_check", schema: 2, gen: 2, updatedBy: "migration-008"},
	}
	require.Equal(t, want, load())

	_, err := pool.Exec(ctx, `UPDATE observation_contract_generations SET updated_by = 'operator-cutover'
		WHERE provider = 'youtubejs' AND observation_kind IN ('live_snapshot', 'video_live_check')`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, pool))

	for i := range want {
		want[i].updatedBy = "operator-cutover"
	}

	require.Equal(t, want, load())
}

package dbtest

import (
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const youtubejsLiveContractGenerationMigration = "008_youtubejs_live_contract_generation.sql"

// 새 DB도 수동 cutover 없이 현행 collector가 발행하는 YouTube.js live 세대를 받아야 하고, 이미 전환된 DB에
// 다시 적용해도 세대 기록을 바꾸지 않아야 한다.
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

	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	migration, err := fs.ReadFile(os.DirFS(dir), youtubejsLiveContractGenerationMigration)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE observation_contract_generations SET updated_by = 'operator-cutover'
		WHERE provider = 'youtubejs' AND observation_kind IN ('live_snapshot', 'video_live_check')`)
	require.NoError(t, err)
	require.NoError(t, applyMigrationContent(ctx, pool, youtubejsLiveContractGenerationMigration, string(migration)))

	for i := range want {
		want[i].updatedBy = "operator-cutover"
	}

	require.Equal(t, want, load())
}

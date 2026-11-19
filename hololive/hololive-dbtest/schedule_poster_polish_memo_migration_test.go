package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulePosterPolishMemoMigrationKeepsOneMemoRow(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `UPDATE schedule_poster_translation_memo SET body = '朝活은 아침 방송', revision = 2, updated_at = now()`)
	require.NoError(t, err)

	require.NoError(t, ApplyMigrations(ctx, pool), "다시 적용해도 실패하지 않는다")

	var (
		rows     int
		body     string
		revision int64
	)

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*), min(body), min(revision) FROM schedule_poster_translation_memo`).Scan(&rows, &body, &revision))
	require.Equal(t, 1, rows)
	require.Equal(t, "朝活은 아침 방송", body, "다시 적용해도 저장한 메모를 덮지 않는다")
	require.Equal(t, int64(2), revision)

	for name, statement := range map[string]string{
		"둘째 행":       `INSERT INTO schedule_poster_translation_memo (id) VALUES (false)`,
		"본문 4001자":   `UPDATE schedule_poster_translation_memo SET body = repeat('가', 4001)`,
		"revision 0": `UPDATE schedule_poster_translation_memo SET revision = 0`,
	} {
		_, err := pool.Exec(ctx, statement)
		require.Error(t, err, name)
	}
}

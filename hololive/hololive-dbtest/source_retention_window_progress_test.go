package dbtest

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const sourceRetentionWindowMigration = "341_source_retention_window_progress.sql"

type retentionWindow struct {
	deleted   int
	passed    int
	completed bool
}

func deleteRetentionWindow(t *testing.T, pool *pgxpool.Pool, cutoff time.Time, limit int) retentionWindow {
	t.Helper()

	var window retentionWindow

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT cardinality(deleted_ids), passed, traversal_completed
		FROM delete_source_observation_retention_window($1,$2,$3)`, []string{communityPageKind}, []time.Time{cutoff}, limit).
		Scan(&window.deleted, &window.passed, &window.completed))

	return window
}

// 창 결과는 삭제 0건인 보호 창의 진척, 삭제 상한을 채운 창의 재개 위치, 순회 완료를 구분한다.
func TestSourceRetentionWindowReportsProgressAndTraversal(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	base := seedBoundedRetention(t, pool, 2500, 500)
	cutoff := base.Add(time.Hour)

	// 보호 500건 뒤 삭제 가능한 300건에서 상한을 채우면, 커서는 마지막 삭제 행까지만 전진한다.
	require.Equal(t, retentionWindow{deleted: 300, passed: 800}, deleteRetentionWindow(t, pool, cutoff, 300))
	require.Equal(t, retentionWindow{deleted: 300, passed: 300}, deleteRetentionWindow(t, pool, cutoff, 300))
	require.Equal(t, retentionWindow{deleted: 1000, passed: 1000}, deleteRetentionWindow(t, pool, cutoff, 1000))
	require.Equal(t, retentionWindow{deleted: 400, passed: 400, completed: true}, deleteRetentionWindow(t, pool, cutoff, 1000))

	// 새 순회는 보호 prefix를 지나며 삭제 0건이어도 진척을 보고하고, 끝에서 다시 완료를 보고한다.
	require.Equal(t, retentionWindow{passed: 500, completed: true}, deleteRetentionWindow(t, pool, cutoff, 1000))
	requireRetentionCount(t, pool, 500)
}

// runtime만 새 창 함수를 실행할 수 있고(PUBLIC 회수는 scraper 거부로 확인), 재적용해도 이전 image가 쓰는 공개 함수의 권한이 그대로다.
func TestSourceRetentionWindowMigrationGrantsRuntimeOnly(t *testing.T) {
	t.Parallel()

	pool := NewPool(t)
	roles := createObservationGrantRoles(t, pool)
	dir, err := resolveMigrationsDir()
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `GRANT EXECUTE ON FUNCTION delete_source_observation_retention_batch(text[],timestamptz[],integer) TO `+
		pgx.Identifier{roles.runtime}.Sanitize())
	require.NoError(t, err)

	raw, err := fs.ReadFile(os.DirFS(dir), sourceRetentionWindowMigration)
	require.NoError(t, err)

	migration := strings.NewReplacer("hololive_runtime", roles.runtime, "hololive_scraper", roles.scraper).Replace(string(raw))

	for range 2 {
		require.NoError(t, applyMigrationContent(t.Context(), pool, sourceRetentionWindowMigration, migration))
	}

	for role, want := range map[string]bool{roles.runtime: true, roles.scraper: false} {
		var window, batch bool

		require.NoError(t, pool.QueryRow(t.Context(), `SELECT
			has_function_privilege($1,'delete_source_observation_retention_window(text[],timestamptz[],integer)','EXECUTE'),
			has_function_privilege($1,'delete_source_observation_retention_batch(text[],timestamptz[],integer)','EXECUTE')`,
			role).Scan(&window, &batch))
		require.Equal(t, want, window, role)
		require.Equal(t, want, batch, role)
	}

	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer rollbackRetentionTransaction(t, tx)

	_, err = tx.Exec(t.Context(), `SET LOCAL ROLE `+pgx.Identifier{roles.runtime}.Sanitize())
	require.NoError(t, err)

	var completed bool

	require.NoError(t, tx.QueryRow(t.Context(), `SELECT traversal_completed FROM delete_source_observation_retention_window(
		ARRAY['community_page']::text[], ARRAY[clock_timestamp()]::timestamptz[], 1)`).Scan(&completed))
	require.True(t, completed)
}

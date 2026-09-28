package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const authSessionGenerationMigration = "231_auth_session_generation.sql"

// 231은 기존 auth_users 행을 다시 쓰지 않고(fast default) session_generation을 0으로 채워야 하며,
// 재적용해도 값·스키마를 바꾸지 않아야 한다.
func TestAuthSessionGenerationMigrationIsMetadataOnlyAndIdempotent(t *testing.T) {
	pool := NewPool(t)
	ctx := t.Context()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}

	if _, err := pool.Exec(ctx, `ALTER TABLE auth_users DROP COLUMN session_generation`); err != nil {
		t.Fatalf("restore pre-231 auth_users: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO auth_users (id, email, password_hash, display_name)
		VALUES ('pre-231-user', 'pre231@example.com', 'hash', 'Pre 231')`); err != nil {
		t.Fatalf("insert pre-231 user: %v", err)
	}

	relfilenodeBefore := authUsersRelfilenode(t, pool)

	for pass := 1; pass <= 2; pass++ {
		if err := applyMigrationFile(ctx, pool, dir, authSessionGenerationMigration); err != nil {
			t.Fatalf("apply %s pass %d: %v", authSessionGenerationMigration, pass, err)
		}

		if got := authUsersRelfilenode(t, pool); got != relfilenodeBefore {
			t.Fatalf("pass %d rewrote auth_users: relfilenode %d -> %d", pass, relfilenodeBefore, got)
		}
	}

	var (
		dataType   string
		notNull    bool
		hasMissing bool
		defaultSQL string
	)

	if err := pool.QueryRow(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod), a.attnotnull, a.atthasmissing, pg_get_expr(d.adbin, d.adrelid)
		FROM pg_attribute a
		JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE a.attrelid = 'auth_users'::regclass AND a.attname = 'session_generation' AND NOT a.attisdropped`,
	).Scan(&dataType, &notNull, &hasMissing, &defaultSQL); err != nil {
		t.Fatalf("load session_generation column: %v", err)
	}

	if dataType != "bigint" || !notNull || defaultSQL != "0" {
		t.Fatalf("session_generation column: type=%s notnull=%v default=%s", dataType, notNull, defaultSQL)
	}

	if !hasMissing {
		t.Fatal("expected existing rows to read the fast default (atthasmissing) instead of a table rewrite")
	}

	var generation int64

	if err := pool.QueryRow(ctx, `SELECT session_generation FROM auth_users WHERE id = 'pre-231-user'`).Scan(&generation); err != nil {
		t.Fatalf("load pre-231 user generation: %v", err)
	}

	if generation != 0 {
		t.Fatalf("pre-231 user generation: got=%d want=0", generation)
	}
}

func authUsersRelfilenode(t *testing.T, pool *pgxpool.Pool) uint32 {
	t.Helper()

	var relfilenode uint32

	if err := pool.QueryRow(t.Context(), `SELECT relfilenode FROM pg_class WHERE oid = 'auth_users'::regclass`).Scan(&relfilenode); err != nil {
		t.Fatalf("load auth_users relfilenode: %v", err)
	}

	return relfilenode
}

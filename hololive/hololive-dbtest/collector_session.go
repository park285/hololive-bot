package dbtest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewCollectorSessionPool은 pool과 같은 데이터베이스에 붙되 연결마다 session_user를 role로 바꾼 pool을 돌려줍니다.
// role은 이 데이터베이스의 public 객체 권한을 모두 받으므로, 이 pool의 거부는 권한이 아니라 trigger에서 나옵니다.
func NewCollectorSessionPool(tb testing.TB, pool *pgxpool.Pool, role string) *pgxpool.Pool {
	tb.Helper()

	if err := grantCollectorSessionRole(tb.Context(), pool, role); err != nil {
		tb.Fatalf("dbtest: prepare collector session role %s: %v", role, err)
	}

	authorize := "SET SESSION AUTHORIZATION " + pgx.Identifier{role}.Sanitize()
	cfg := pool.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, authorize); err != nil {
			return fmt.Errorf("set collector session authorization: %w", err)
		}

		return nil
	}

	//nolint:usetesting // pool은 t.Cleanup에서 닫히므로 테스트 종료와 함께 취소되는 t.Context()에 묶지 않는다.
	sessionPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		tb.Fatalf("dbtest: open collector session pool %s: %v", role, err)
	}

	tb.Cleanup(sessionPool.Close)

	return sessionPool
}

func grantCollectorSessionRole(ctx context.Context, pool *pgxpool.Pool, role string) error {
	quoted := pgx.Identifier{role}.Sanitize()

	if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN"); err != nil && !isDuplicateRole(err) {
		return fmt.Errorf("create role: %w", err)
	}

	for _, grant := range []string{
		"GRANT USAGE ON SCHEMA public TO " + quoted,
		"GRANT ALL ON ALL TABLES IN SCHEMA public TO " + quoted,
		"GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO " + quoted,
		"GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO " + quoted,
	} {
		if _, err := pool.Exec(ctx, grant); err != nil {
			return fmt.Errorf("grant: %w", err)
		}
	}

	return nil
}

// role은 클러스터 전역이라 병렬 패키지가 같은 이름을 동시에 만들 수 있고, 그 경쟁은 42710 또는 23505로 끝난다.
func isDuplicateRole(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)

	return ok && (pgErr.Code == "42710" || pgErr.Code == "23505")
}

package dbtest

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

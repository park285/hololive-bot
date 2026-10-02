package sourceobservation

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertTableCount는 테스트 DB의 테이블 행 수를 확인한다. 테이블 이름을 인자로 받으므로 SQL 자산 대신 테스트 파일에 둔다.
func assertTableCount(t *testing.T, pool *pgxpool.Pool, table string, want int) {
	t.Helper()

	var count int

	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

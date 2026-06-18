// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// testDatabaseURLEnv가 설정되면 testcontainers 대신 해당 DSN을 base로 쓴다.
	testDatabaseURLEnv = "TEST_DATABASE_URL"

	// primaryImage는 ephemeral PG 기동에 우선 사용할 이미지다(호스트에 postgres:18 존재).
	primaryImage = "postgres:18-alpine"

	// fallbackImage는 primaryImage pull 실패 시 사용할 이미지다.
	fallbackImage = "postgres:16-alpine"
)

// baseProvider는 테스트 바이너리당 1개의 base DSN(컨테이너 또는 외부 DB)을 lazily 확보한다.
type baseProvider struct {
	once sync.Once
	dsn  string
	err  error
}

var sharedBase baseProvider

// dbSeq는 격리 데이터베이스 이름 충돌 방지용 카운터다.
var dbSeq atomic.Uint64

// NewPool은 격리된 데이터베이스를 가진 *pgxpool.Pool을 반환한다.
//
// 동작:
//   - TEST_DATABASE_URL이 있으면 그 DSN을, 없으면 testcontainers ephemeral PG를 base로 쓴다.
//     base는 sync.Once로 테스트 바이너리당 1회만 확보된다(컨테이너 재기동 없음).
//   - 호출마다 고유 데이터베이스(test_<unique>)를 생성하고, 그 DB에 연결된 pool에
//     manifest 전체(006 base 포함)를 순서대로 적용해 반환한다.
//   - t.Cleanup에 DROP DATABASE와 pool close를 등록한다.
//
// manifest 전체가 빈 DB에서 재생되는 이유: 006-base-runtime-tables.sql이 레거시
// 초기 DB 생성 경로의 base 테이블(members, alarms 등)을 manifest 최초 단계에서
// 멱등 복원한다. 따라서 과거의 base-schema gap이 사라졌고 manifest 전체 chain을
// 그대로 적용한다.
//
// per-schema가 아닌 per-database 격리를 쓰는 이유: prod migration 다수가 idempotent guard로
// information_schema를 table_schema 한정 없이 조회한다(예: 037이 acl_rooms.list_type 존재 여부를
// 전체 카탈로그에서 확인). 단일 DB 내 여러 스키마로 격리하면 한 스키마의 변경이 다른 스키마의
// guard 판정을 오염시킨다. DB 단위로 격리하면 카탈로그가 완전히 분리되어 guard가 정확히 동작한다.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()

	baseDSN := acquireBaseDSN(t)

	ctx := context.Background()

	dbName := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), dbSeq.Add(1))

	createIsolatedDatabase(t, ctx, baseDSN, dbName)

	pool := openTestPool(t, ctx, baseDSN, dbName)

	t.Cleanup(func() {
		if dropErr := dropDatabase(ctx, baseDSN, dbName); dropErr != nil {
			t.Errorf("dbtest: drop database %s: %v", dbName, dropErr)
		}
	})
	t.Cleanup(pool.Close)

	if err := ApplyMigrations(ctx, pool); err != nil {
		t.Fatalf("dbtest: apply migrations to database %s: %v", dbName, err)
	}

	return pool
}

// createIsolatedDatabase는 base DSN의 기본 데이터베이스에 admin pool로 연결해
// 격리 DB(dbName)를 생성한다. 식별자는 내부 생성(time+seq)이라 인젝션 위험이 없으나
// quote로 안전하게 감싼다.
func createIsolatedDatabase(t testing.TB, ctx context.Context, baseDSN, dbName string) {
	t.Helper()

	adminPool, err := poolForDatabase(ctx, baseDSN, "")
	if err != nil {
		t.Fatalf("dbtest: connect base for database setup: %v", err)
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(dbName))); err != nil {
		t.Fatalf("dbtest: create database %s: %v", dbName, err)
	}
}

// openTestPool은 격리 DB에 연결된 pool을 반환한다. 연결 실패 시 best-effort로
// 해당 DB를 drop한 뒤 t.Fatalf로 종료한다.
func openTestPool(t testing.TB, ctx context.Context, baseDSN, dbName string) *pgxpool.Pool {
	t.Helper()

	pool, err := poolForDatabase(ctx, baseDSN, dbName)
	if err != nil {
		if dropErr := dropDatabase(ctx, baseDSN, dbName); dropErr != nil {
			t.Errorf("dbtest: drop database %s after test pool failure: %v", dbName, dropErr)
		}

		t.Fatalf("dbtest: connect test database pool: %v", err)
	}

	return pool
}

// acquireBaseDSN은 공유 base DSN을 반환한다(최초 1회 확보).
func acquireBaseDSN(t testing.TB) string {
	t.Helper()

	sharedBase.once.Do(func() {
		sharedBase.dsn, sharedBase.err = provisionBaseDSN()
	})

	if sharedBase.err != nil {
		t.Fatalf("dbtest: provision base database: %v", sharedBase.err)
	}

	return sharedBase.dsn
}

// provisionBaseDSN은 외부 DB(TEST_DATABASE_URL) 또는 testcontainers ephemeral PG의 DSN을 만든다.
// 컨테이너는 프로세스 종료 시 testcontainers reaper(Ryuk)가 회수하므로 명시적 종료를 등록하지 않는다.
func provisionBaseDSN() (string, error) {
	if dsn := os.Getenv(testDatabaseURLEnv); dsn != "" {
		return dsn, nil
	}

	ctx := context.Background()

	container, err := startPostgresContainer(ctx, primaryImage)
	if err != nil {
		// primary 이미지 기동 실패 시 fallback 이미지로 재시도.
		container, err = startPostgresContainer(ctx, fallbackImage)
		if err != nil {
			return "", fmt.Errorf("start postgres container (%s, %s): %w", primaryImage, fallbackImage, err)
		}
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", fmt.Errorf("connection string: %w", err)
	}

	return dsn, nil
}

func startPostgresContainer(ctx context.Context, image string) (*postgres.PostgresContainer, error) {
	return postgres.Run(ctx, image,
		postgres.WithDatabase("dbtest"),
		postgres.WithUsername("dbtest"),
		postgres.WithPassword("dbtest"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
}

// dropDatabase는 격리 데이터베이스를 제거한다(cleanup 경로). best-effort지만 에러를
// 반환하여 호출자가 visible하게 보고할 수 있게 한다.
//
// 우선 DROP DATABASE ... WITH (FORCE)(PG 13+)로 잔여 연결까지 끊고 제거한다. FORCE가
// 실패하면(PG<13 syntax 미지원 또는 그 외) 잔여 연결을 pg_terminate_backend로 정리한 뒤
// 일반 DROP DATABASE를 시도한다.
func dropDatabase(ctx context.Context, baseDSN, dbName string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := poolForDatabase(ctx, baseDSN, "")
	if err != nil {
		return fmt.Errorf("connect base for drop %s: %w", dbName, err)
	}
	defer pool.Close()

	if _, forceErr := pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName))); forceErr == nil {
		return nil
	}

	// FORCE 미지원/실패 fallback: 잔여 연결을 끊고 일반 DROP을 시도한다.
	if _, termErr := pool.Exec(ctx,
		`SELECT pg_terminate_backend(pid)
		 FROM pg_stat_activity
		 WHERE datname = $1 AND pid <> pg_backend_pid()`,
		dbName,
	); termErr != nil {
		return fmt.Errorf("terminate backends on %s: %w", dbName, termErr)
	}

	if _, dropErr := pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(dbName))); dropErr != nil {
		return fmt.Errorf("drop database %s: %w", dbName, dropErr)
	}

	return nil
}

// poolForDatabase는 base DSN을 파싱해 데이터베이스명만 교체한 *pgxpool.Pool을 만든다.
// dbName이 빈 문자열이면 base DSN의 데이터베이스를 그대로 쓴다.
//
// url.Parse 대신 pgxpool.ParseConfig를 쓰는 이유: TEST_DATABASE_URL이 URL 형식
// (postgres://...)뿐 아니라 libpq keyword 형식(host=... dbname=...)일 수 있는데,
// url.Parse는 keyword DSN을 깨뜨린다. ParseConfig는 두 형식을 모두 처리한다.
func poolForDatabase(ctx context.Context, baseDSN, dbName string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(baseDSN)
	if err != nil {
		return nil, fmt.Errorf("parse base dsn: %w", err)
	}

	if dbName != "" {
		cfg.ConnConfig.Database = dbName
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}

	return pool, nil
}

// quoteIdent는 SQL 식별자를 큰따옴표로 감싸 안전하게 인용한다.
func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

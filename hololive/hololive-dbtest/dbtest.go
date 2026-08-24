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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// TEST_DATABASE_URL이 설정되면 testcontainers 대신 해당 DSN을 base로 쓴다.
	testDatabaseURLEnv        = "TEST_DATABASE_URL"
	testDatabaseOwnerTokenEnv = "TEST_DATABASE_OWNER_TOKEN"
	allowExternalTestDBEnv    = "ALLOW_EXTERNAL_TEST_DB"
	ownershipSentinelQuery    = "SELECT token FROM ci_ephemeral_sentinel LIMIT 1"

	// PostgreSQL 18 image로, production migration 기준과 같은 태그를 고정한다.
	postgresImage = "postgres:18.6-alpine@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2"
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

func NewReplayPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()

	ctx := tb.Context()
	pool := NewBlankPool(tb)

	if err := ApplyMigrations(ctx, pool); err != nil {
		tb.Fatalf("dbtest: apply migrations: %v", err)
	}

	return pool
}

func NewBlankPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()

	baseDSN := acquireBaseDSN(tb)

	return newIsolatedPool(tb, baseDSN, "")
}

func newIsolatedPool(tb testing.TB, baseDSN, templateName string) *pgxpool.Pool {
	tb.Helper()

	//nolint:usetesting // 이 컨텍스트는 t.Cleanup의 데이터베이스 정리에서 재사용되므로, 테스트 종료와 함께 취소되는 t.Context()를 쓸 수 없다.
	ctx := context.Background()
	dbName := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), dbSeq.Add(1))

	if err := createIsolatedDatabase(ctx, baseDSN, dbName, templateName); err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	pool, err := openTestPool(ctx, baseDSN, dbName)
	if err != nil {
		tb.Fatalf("dbtest: %v", err)
	}

	tb.Cleanup(func() {
		if dropErr := dropDatabase(ctx, baseDSN, dbName); dropErr != nil {
			tb.Errorf("dbtest: drop database %s: %v", dbName, dropErr)
		}
	})
	tb.Cleanup(pool.Close)

	return pool
}

// createIsolatedDatabase는 base DSN의 기본 데이터베이스에 admin pool로 연결해
// 격리 DB(dbName)를 생성한다. 식별자는 내부 생성(time+seq)이라 인젝션 위험이 없으나
// quote로 안전하게 감싼다.
func createIsolatedDatabase(ctx context.Context, baseDSN, dbName, templateName string) error {
	adminPool, err := poolForDatabase(ctx, baseDSN, "")
	if err != nil {
		return fmt.Errorf("connect base for database setup: %w", err)
	}
	defer adminPool.Close()

	statement := fmt.Sprintf("CREATE DATABASE %s", quoteIdent(dbName))

	if templateName != "" {
		statement += " TEMPLATE " + quoteIdent(templateName)
	}

	if _, err := adminPool.Exec(ctx, statement); err != nil {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}

	return nil
}

// openTestPool은 격리 DB에 연결된 pool을 반환한다. 연결 실패 시 best-effort로
// 해당 DB를 drop한 뒤 원인 오류를 반환한다.
func openTestPool(ctx context.Context, baseDSN, dbName string) (*pgxpool.Pool, error) {
	pool, err := poolForDatabase(ctx, baseDSN, dbName)
	if err == nil {
		return pool, nil
	}

	connectErr := fmt.Errorf("connect test database pool: %w", err)

	if dropErr := dropDatabase(ctx, baseDSN, dbName); dropErr != nil {
		return nil, errors.Join(connectErr, fmt.Errorf("drop database %s after test pool failure: %w", dbName, dropErr))
	}

	return nil, connectErr
}

// acquireBaseDSN은 공유 base DSN을 반환한다(최초 1회 확보).
func acquireBaseDSN(tb testing.TB) string {
	tb.Helper()

	sharedBase.once.Do(func() {
		sharedBase.dsn, sharedBase.err = provisionBaseDSN()
	})

	if sharedBase.err != nil {
		tb.Fatalf("dbtest: provision base database: %v", sharedBase.err)
	}

	return sharedBase.dsn
}

// provisionBaseDSN은 외부 DB(TEST_DATABASE_URL) 또는 testcontainers ephemeral PG의 DSN을 만든다.
// 컨테이너는 프로세스 종료 시 testcontainers reaper(Ryuk)가 회수하므로 명시적 종료를 등록하지 않는다.
func provisionBaseDSN() (_ string, err error) {
	if dsn := os.Getenv(testDatabaseURLEnv); dsn != "" {
		out, presetErr := validatedPresetDSN(dsn)
		if presetErr != nil {
			return out, fmt.Errorf("validated preset DSN: %w", presetErr)
		}

		return out, nil
	}

	ctx := context.Background()

	unlock, err := lockSessionProvisioning()
	if err != nil {
		return "", fmt.Errorf("lock session provisioning: %w", err)
	}

	defer func() { err = errors.Join(err, unlock()) }()

	container, err := provisionPostgresContainer(
		ctx,
		postgresImage,
		startPostgresContainer,
		holdExistingSessionReaper,
		ensureVerifiedReaperClient,
	)
	if err != nil {
		return "", fmt.Errorf("start postgres container %s: %w", postgresImage, err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return "", fmt.Errorf("connection string: %w", err)
	}

	return dsn, nil
}

func holdExistingSessionReaper(ctx context.Context) error {
	if verifiedReaperConn != nil {
		return nil
	}

	_, found, err := findSessionReaper(ctx)
	if err != nil {
		if isTransientReaperError(err) {
			return nil
		}

		return fmt.Errorf("find session reaper: %w", err)
	}

	if !found {
		return nil
	}

	if err := ensureVerifiedReaperClient(ctx); err != nil {
		return fmt.Errorf("ensure verified reaper client: %w", err)
	}

	return nil
}

func validatedPresetDSN(dsn string) (string, error) {
	if err := verifyPresetDSNOwnership(dsn); err != nil {
		return "", fmt.Errorf("verify preset DSN ownership: %w", err)
	}

	return dsn, nil
}

func verifyPresetDSNOwnership(dsn string) error {
	expectedToken := strings.TrimSpace(os.Getenv(testDatabaseOwnerTokenEnv))
	allowExternal := os.Getenv(allowExternalTestDBEnv) == "true"

	if expectedToken == "" {
		if err := validateOwnershipEvidence("", "", nil, allowExternal); err != nil {
			return fmt.Errorf("validate ownership evidence: %w", err)
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := poolForDatabase(ctx, dsn, "")
	if err != nil {
		return fmt.Errorf("verify preset database ownership: %w", err)
	}
	defer pool.Close()

	var gotToken string

	queryErr := pool.QueryRow(ctx, ownershipSentinelQuery).Scan(&gotToken)

	if err := validateOwnershipEvidence(expectedToken, gotToken, queryErr, allowExternal); err != nil {
		return fmt.Errorf("validate ownership evidence: %w", err)
	}

	return nil
}

func validateOwnershipEvidence(expectedToken, gotToken string, queryErr error, allowExternal bool) error {
	if expectedToken == "" {
		if allowExternal {
			return nil
		}

		return fmt.Errorf("dbtest: unproven database ownership; use testcontainers or opt in to a dedicated disposable server with %s=true", allowExternalTestDBEnv)
	}

	if queryErr != nil {
		return fmt.Errorf("dbtest: read ownership sentinel: %w", queryErr)
	}

	if gotToken != expectedToken {
		return errors.New("dbtest: ownership sentinel mismatch")
	}

	return nil
}

// 같은 go test 호출의 테스트 바이너리들은 testcontainers 세션(parent pid 기반)을 공유해
// reaper(Ryuk) 컨테이너 1개를 같이 쓴다. 여러 바이너리가 첫 컨테이너 생성을 동시에 시작하면
// reaper 기동과 재사용 조회가 경합해 늦게 진입한 프로세스의 reaper 연결이 소리 없이 유실되고,
// Ryuk이 클라이언트 0으로 오판해 reconnection timeout(10s) 뒤 실행 중인 다른 바이너리의
// PG 컨테이너까지 세션 라벨로 일괄 회수한다. SessionID()가 UUID fallback이 되어도
// 같은 호출을 직렬화하도록 parent pid로 flock한다.
func sessionProvisionLockPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dbtest-provision-%d.lock", os.Getppid()))
}

func lockSessionProvisioning() (func() error, error) {
	path := sessionProvisionLockPath()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // TempDir와 parent pid로만 구성되는 test lock path입니다.
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}

	if flockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); flockErr != nil {
		err := fmt.Errorf("flock %s: %w", path, flockErr)

		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close lock file: %w", closeErr))
		}

		return nil, err
	}

	return func() error {
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close lock file %s: %w", path, closeErr)
		}

		return nil
	}, nil
}

// dropDatabase는 격리 데이터베이스를 제거한다(cleanup 경로). 실패해도 진행하는 best-effort
// 경로지만, 에러를 반환하여 호출자가 visible하게 보고할 수 있게 한다.
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
// 빈 dbName은 base DSN의 데이터베이스를 그대로 쓴다는 뜻이다.
//
// URL 형식과 libpq keyword 형식(host=, dbname=)을 모두 받아야 하므로 url.Parse가 아니라
// ParseConfig를 쓴다. TEST_DATABASE_URL이 keyword DSN으로 오면 url.Parse는 이를 깨뜨리지만,
// ParseConfig는 두 형식을 모두 처리한다.
func poolForDatabase(ctx context.Context, baseDSN, dbName string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(baseDSN)
	if err != nil {
		return nil, fmt.Errorf("parse base dsn: %w", err)
	}

	cfg.ConnConfig.DefaultQueryExecMode = productionQueryExecMode

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

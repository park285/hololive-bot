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
	testDatabaseURLEnv        = "TEST_DATABASE_URL"
	testDatabaseOwnerTokenEnv = "TEST_DATABASE_OWNER_TOKEN"
	allowExternalTestDBEnv    = "ALLOW_EXTERNAL_TEST_DB"
	ownershipSentinelQuery    = "SELECT token FROM ci_ephemeral_sentinel LIMIT 1"

	postgresImage = "postgres:18.7-alpine@sha256:b6972e1115a4ff8eba5d7f7d19184926d1e16d7d1136ae1e1a7ebf286da0f015"
)

type baseProvider struct {
	once sync.Once
	dsn  string
	err  error
}

var sharedBase baseProvider

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

func sessionProvisionLockPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("dbtest-provision-%d.lock", os.Getppid()))
}

func lockSessionProvisioning() (func() error, error) {
	path := sessionProvisionLockPath()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- TempDir와 parent pid로만 구성되는 test lock path입니다.
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

func dropDatabase(ctx context.Context, baseDSN, dbName string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := poolForDatabase(ctx, baseDSN, "")
	if err != nil {
		return fmt.Errorf("connect base for drop %s: %w", dbName, err)
	}
	defer pool.Close()

	if _, dropErr := pool.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(dbName))); dropErr != nil {
		return fmt.Errorf("drop database %s with force: %w", dbName, dropErr)
	}

	return nil
}

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

func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

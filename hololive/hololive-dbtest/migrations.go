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

// Package dbtest는 pgx 마이그레이션 테스트를 위한 PostgreSQL 하니스를 제공한다.
// prod SSOT migration(hololive-api/scripts/migrations)을 격리 스키마에 그대로
// 적용하여 test/prod schema drift를 제거한다.
package dbtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

func migrationFingerprint() (string, error) {
	dir, err := resolveMigrationsDir()
	if err != nil {
		return "", fmt.Errorf("resolve migrations dir: %w", err)
	}

	manifestPath := filepath.Join(dir, manifestFileName)

	entries, err := readManifest(manifestPath)
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}

	migrationFiles := os.DirFS(dir)
	hash := sha256.New()

	manifest, err := fs.ReadFile(migrationFiles, manifestFileName)
	if err != nil {
		return "", fmt.Errorf("read migration manifest fingerprint: %w", err)
	}

	_, _ = hash.Write(manifest)

	for _, filename := range entries {
		content, err := fs.ReadFile(migrationFiles, filename)
		if err != nil {
			return "", fmt.Errorf("read migration fingerprint %s: %w", filename, err)
		}

		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(filename))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(content)
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

const (
	// 모노레포 루트를 식별하는 파일이다.
	repoRootMarker = "build-all.sh"

	// 모노레포 루트 기준 prod migration SSOT 경로다.
	migrationsRelDir = "hololive/hololive-api/scripts/migrations"

	// 적용 순서를 규정하는 파일이다("NNN filename.sql" 형식).
	manifestFileName = "manifest.txt"

	// 자동 탐색을 우회하는 override env다.
	migrationsDirEnv = "HOLOLIVE_MIGRATIONS_DIR"

	epoch2BaselineMigration = "001_schema_epoch2_baseline.sql"
	dbtestLedgerSchema      = "hololive_dbtest_internal"
)

// ApplyMigrations는 manifest.txt 순서대로 prod migration SQL을 pool이 가리키는
// (search_path 설정된) 스키마에 적용한다.
//
// 디렉터리 탐색 우선순위: HOLOLIVE_MIGRATIONS_DIR env → CWD에서 위로 build-all.sh
// 마커를 찾아 migrationsRelDir append.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	dir, err := resolveMigrationsDir()
	if err != nil {
		return fmt.Errorf("apply migrations: resolve dir: %w", err)
	}

	entries, err := readManifest(filepath.Join(dir, manifestFileName))
	if err != nil {
		return fmt.Errorf("apply migrations: read manifest: %w", err)
	}

	if err := ensureDBTestMigrationLedger(ctx, pool); err != nil {
		return fmt.Errorf("ensure DB test migration ledger: %w", err)
	}

	for _, filename := range entries {
		if err := applyManifestMigration(ctx, pool, dir, filename); err != nil {
			return fmt.Errorf("apply manifest migration: %w", err)
		}
	}

	return nil
}

func ensureDBTestMigrationLedger(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+dbtestLedgerSchema); err != nil {
		return fmt.Errorf("apply migrations: create dbtest ledger schema: %w", err)
	}

	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+dbtestLedgerSchema+`.schema_migrations (
		filename text PRIMARY KEY,
		checksum_sha256 char(64) NOT NULL CHECK (checksum_sha256 ~ '^[0-9a-f]{64}$'),
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("apply migrations: create dbtest ledger: %w", err)
	}

	return nil
}

func applyManifestMigration(ctx context.Context, pool *pgxpool.Pool, dir, filename string) error {
	raw, err := fs.ReadFile(os.DirFS(dir), filename)
	if err != nil {
		return fmt.Errorf("apply migrations: read %s: %w", filename, err)
	}

	digest := sha256.Sum256(raw)
	checksum := hex.EncodeToString(digest[:])

	stored, applied, err := dbtestMigrationChecksum(ctx, pool, filename)
	if err != nil {
		return fmt.Errorf("dbtest migration checksum: %w", err)
	}

	if applied {
		if stored != checksum {
			return fmt.Errorf("apply migrations: %s checksum mismatch: ledger=%s source=%s", filename, stored, checksum)
		}

		return nil
	}

	if err := applyUnrecordedMigration(ctx, pool, filename, string(raw), checksum); err != nil {
		return fmt.Errorf("apply unrecorded migration: %w", err)
	}

	return nil
}

func applyUnrecordedMigration(ctx context.Context, pool *pgxpool.Pool, filename, content, checksum string) error {
	if filename == epoch2BaselineMigration {
		if err := applyAtomicBaseline(ctx, pool, filename, content, checksum); err != nil {
			return fmt.Errorf("apply atomic baseline: %w", err)
		}

		return nil
	}

	if err := applyMigrationContent(ctx, pool, filename, content); err != nil {
		return fmt.Errorf("apply migration content: %w", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO `+dbtestLedgerSchema+`.schema_migrations (filename, checksum_sha256) VALUES ($1, $2)`, filename, checksum); err != nil {
		return fmt.Errorf("apply migrations: record %s: %w", filename, err)
	}

	return nil
}

func dbtestMigrationChecksum(ctx context.Context, pool *pgxpool.Pool, filename string) (checksum string, applied bool, resultErr error) {
	queryErr := pool.QueryRow(ctx, `SELECT checksum_sha256::text FROM `+dbtestLedgerSchema+`.schema_migrations WHERE filename = $1`, filename).Scan(&checksum)
	if errors.Is(queryErr, pgx.ErrNoRows) {
		return "", false, nil
	}

	if queryErr != nil {
		return "", false, fmt.Errorf("apply migrations: query dbtest ledger %s: %w", filename, queryErr)
	}

	return checksum, true, nil
}

func applyAtomicBaseline(ctx context.Context, pool *pgxpool.Pool, filename, content, checksum string) error {
	segments, err := sqlsplit.Segments(content)
	if err != nil {
		return fmt.Errorf("apply migrations: split %s: %w", filename, err)
	}

	if len(segments) != 1 || !segments[0].Transactional {
		return fmt.Errorf("apply migrations: %s must be one top-level transaction", filename)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: acquire tx connection for %s: %w", filename, err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: begin %s: %w", filename, err)
	}

	if err := applyTxStatements(ctx, tx, filename, segments[0].Statements); err != nil {
		return fmt.Errorf("rollback migration tx on error: %w", rollbackMigrationTxOnError(ctx, tx, err))
	}

	if _, err := tx.Exec(ctx, `INSERT INTO `+dbtestLedgerSchema+`.schema_migrations (filename, checksum_sha256) VALUES ($1, $2)`, filename, checksum); err != nil {
		return fmt.Errorf("rollback migration tx on error: %w", rollbackMigrationTxOnError(ctx, tx, fmt.Errorf("apply migrations: record %s: %w", filename, err)))
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("apply migrations: commit %s: %w", filename, err)
	}

	return nil
}

// applyMigrationFile은 단일 migration 파일을 읽어 statement 단위로 적용한다.
//
// 각 statement를 개별 Exec한다. 멀티-statement 문자열을 pool.Exec에 넘기면
// simple query protocol이 암묵 트랜잭션 블록으로 감싸 CREATE INDEX CONCURRENTLY가
// "cannot run inside a transaction block"으로 실패한다(019/060/061). 반대로 statement
// 단위로 보내면 각 statement가 autocommit으로 실행되어 CONCURRENTLY가 동작한다.
// 소스의 top-level BEGIN;/COMMIT; 블록은 prod Go 러너(hololive-api migrationrunner)와
// 동일하게 단일 커넥션의 실제 트랜잭션으로 감싼다 — pool.Exec로 BEGIN을 보내면
// pgxpool이 tx 상태 커넥션을 release 시 파기해 블록이 침묵 해체된다.
func applyMigrationFile(ctx context.Context, pool *pgxpool.Pool, dir, filename string) error {
	sql, readErr := fs.ReadFile(os.DirFS(dir), filename)
	if readErr != nil {
		return fmt.Errorf("apply migrations: read %s: %w", filename, readErr)
	}

	if err := applyMigrationContent(ctx, pool, filename, string(sql)); err != nil {
		return fmt.Errorf("apply migration content: %w", err)
	}

	return nil
}

func applyMigrationContent(ctx context.Context, pool *pgxpool.Pool, filename, content string) error {
	segments, splitErr := sqlsplit.Segments(content)
	if splitErr != nil {
		return fmt.Errorf("apply migrations: split %s: %w", filename, splitErr)
	}

	for _, segment := range segments {
		if err := applyMigrationSegment(ctx, pool, filename, segment); err != nil {
			return fmt.Errorf("apply migration segment: %w", err)
		}
	}

	return nil
}

func applyMigrationSegment(ctx context.Context, pool *pgxpool.Pool, filename string, segment sqlsplit.Segment) error {
	if segment.Transactional {
		if err := applyMigrationTxSegment(ctx, pool, filename, segment.Statements); err != nil {
			return fmt.Errorf("apply migration tx segment: %w", err)
		}

		return nil
	}

	for _, stmt := range segment.Statements {
		if _, execErr := pool.Exec(ctx, stmt); execErr != nil {
			return fmt.Errorf("apply migrations: exec %s: %w", filename, execErr)
		}
	}

	return nil
}

func applyMigrationTxSegment(ctx context.Context, pool *pgxpool.Pool, filename string, statements []string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: acquire tx connection for %s: %w", filename, err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: begin %s: %w", filename, err)
	}

	if err := applyTxStatements(ctx, tx, filename, statements); err != nil {
		if rollbackErr := rollbackMigrationTxOnError(ctx, tx, err); rollbackErr != nil {
			return fmt.Errorf("rollback migration tx on error: %w", rollbackErr)
		}

		return nil
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("apply migrations: commit %s: %w", filename, err)
	}

	return nil
}

func applyTxStatements(ctx context.Context, tx pgx.Tx, filename string, statements []string) error {
	for _, stmt := range statements {
		if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
			return fmt.Errorf("apply migrations: exec %s: %w", filename, execErr)
		}
	}

	return nil
}

func rollbackMigrationTxOnError(ctx context.Context, tx pgx.Tx, err error) error {
	if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return errors.Join(err, fmt.Errorf("rollback migration tx: %w", rollbackErr))
	}

	return err
}

// resolveMigrationsDir는 migration 디렉터리 절대 경로를 결정한다.
func resolveMigrationsDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv(migrationsDirEnv)); env != "" {
		return env, nil
	}

	root, err := findRepoRoot()
	if err != nil {
		return "", fmt.Errorf("find repo root: %w", err)
	}

	return filepath.Join(root, migrationsRelDir), nil
}

// findRepoRoot는 CWD에서 위로 올라가며 build-all.sh 마커를 가진 디렉터리를 찾는다.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	dir := cwd

	for {
		if _, statErr := os.Stat(filepath.Join(dir, repoRootMarker)); statErr == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repo root marker %q not found above %q (set %s)", repoRootMarker, cwd, migrationsDirEnv)
		}

		dir = parent
	}
}

// readManifest는 manifest.txt를 읽어 적용 순서대로 .sql 파일명 슬라이스를 반환한다.
// 각 라인은 "NNN filename.sql" 형식이며, 빈 줄과 '#' 주석은 무시한다.
func readManifest(path string) ([]string, error) {
	dir, name := filepath.Split(path)

	content, err := fs.ReadFile(os.DirFS(dir), name)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var filenames []string

	for line := range strings.SplitSeq(string(content), "\n") {
		name, skip, parseErr := parseManifestLine(line)
		if parseErr != nil {
			return nil, fmt.Errorf("parse manifest line: %w", parseErr)
		}

		if skip {
			continue
		}

		filenames = append(filenames, name)
	}

	if len(filenames) == 0 {
		return nil, fmt.Errorf("manifest %q has no entries", path)
	}

	return filenames, nil
}

// parseManifestLine은 manifest 한 줄을 파싱한다. 빈 줄·'#' 주석은 skip=true,
// "NNN filename.sql" 형식이면 마지막 필드(파일명)를 반환한다.
func parseManifestLine(raw string) (name string, skip bool, err error) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", true, nil
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", false, fmt.Errorf("malformed manifest line %q (want \"NNN filename.sql\")", line)
	}

	return fields[len(fields)-1], false, nil
}

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
// prod SSOT migration(hololive-kakao-bot-go/scripts/migrations)을 격리 스키마에 그대로
// 적용하여 test/prod schema drift를 제거한다.
package dbtest

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// repoRootMarker는 hololive-bot 모노레포 루트를 식별하는 파일이다.
	repoRootMarker = "build-all.sh"

	// migrationsRelDir는 모노레포 루트 기준 prod migration SSOT 경로다.
	migrationsRelDir = "hololive/hololive-kakao-bot-go/scripts/migrations"

	// manifestFileName은 적용 순서를 규정하는 파일이다("NNN filename.sql" 형식).
	manifestFileName = "manifest.txt"

	// migrationsDirEnv는 자동 탐색을 우회하는 override env다.
	migrationsDirEnv = "HOLOLIVE_MIGRATIONS_DIR"
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

	for _, filename := range entries {
		if err := applyMigrationFile(ctx, pool, filepath.Join(dir, filename), filename); err != nil {
			return err
		}
	}

	return nil
}

// applyMigrationFile은 단일 migration 파일을 읽어 statement 단위로 적용한다.
//
// 각 statement를 개별 Exec한다. pool.Exec에 멀티-statement 문자열을 넘기면
// simple query protocol이 암묵 트랜잭션 블록으로 감싸 CREATE INDEX CONCURRENTLY가
// "cannot run inside a transaction block"으로 실패한다(019/060/061). statement
// 단위로 보내면 각 statement가 autocommit으로 실행되어 CONCURRENTLY가 동작한다.
func applyMigrationFile(ctx context.Context, pool *pgxpool.Pool, path, filename string) error {
	sql, readErr := os.ReadFile(path)
	if readErr != nil {
		return fmt.Errorf("apply migrations: read %s: %w", filename, readErr)
	}

	for _, stmt := range splitSQLStatements(string(sql)) {
		if _, execErr := pool.Exec(ctx, stmt); execErr != nil {
			return fmt.Errorf("apply migrations: exec %s: %w", filename, execErr)
		}
	}

	return nil
}

// resolveMigrationsDir는 migration 디렉터리 절대 경로를 결정한다.
func resolveMigrationsDir() (string, error) {
	if env := strings.TrimSpace(os.Getenv(migrationsDirEnv)); env != "" {
		return env, nil
	}

	root, err := findRepoRoot()
	if err != nil {
		return "", err
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
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = file.Close() }()

	var filenames []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		name, skip, parseErr := parseManifestLine(scanner.Text())
		if parseErr != nil {
			return nil, parseErr
		}
		if skip {
			continue
		}

		filenames = append(filenames, name)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan manifest: %w", scanErr)
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

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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

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

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

// MigrationFilter는 적용할 migration 파일을 선별한다.
// 파일명(예: "037_acl_blacklist_mode.sql")을 받아 false를 반환하면 해당 파일을 건너뛴다.
// nil이면 manifest의 모든 파일을 적용한다.
type MigrationFilter func(filename string) bool

// applyOptions는 ApplyMigrations의 동작을 조정한다.
type applyOptions struct {
	dir    string
	filter MigrationFilter
}

// ApplyOption은 ApplyMigrations 동작을 조정하는 함수형 옵션이다.
type ApplyOption func(*applyOptions)

// WithMigrationsDir는 migration 디렉터리를 명시적으로 지정한다(자동 탐색·env override 우선).
func WithMigrationsDir(dir string) ApplyOption {
	return func(o *applyOptions) {
		o.dir = dir
	}
}

// WithMigrationFilter는 적용할 migration 파일을 선별하는 필터를 설정한다.
func WithMigrationFilter(filter MigrationFilter) ApplyOption {
	return func(o *applyOptions) {
		o.filter = filter
	}
}

// ApplyMigrations는 manifest.txt 순서대로 prod migration SQL을 pool이 가리키는
// (search_path 설정된) 스키마에 적용한다.
//
// 디렉터리 탐색 우선순위: WithMigrationsDir 옵션 → HOLOLIVE_MIGRATIONS_DIR env →
// CWD에서 위로 build-all.sh 마커를 찾아 migrationsRelDir append.
func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool, opts ...ApplyOption) error {
	options := &applyOptions{}
	for _, opt := range opts {
		opt(options)
	}

	dir, err := resolveMigrationsDir(options.dir)
	if err != nil {
		return fmt.Errorf("apply migrations: resolve dir: %w", err)
	}

	entries, err := readManifest(filepath.Join(dir, manifestFileName))
	if err != nil {
		return fmt.Errorf("apply migrations: read manifest: %w", err)
	}

	for _, filename := range entries {
		if options.filter != nil && !options.filter(filename) {
			continue
		}

		path := filepath.Join(dir, filename)

		sql, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("apply migrations: read %s: %w", filename, readErr)
		}

		// 각 statement를 개별 Exec한다. pool.Exec에 멀티-statement 문자열을 넘기면
		// simple query protocol이 암묵 트랜잭션 블록으로 감싸 CREATE INDEX CONCURRENTLY가
		// "cannot run inside a transaction block"으로 실패한다(019/060/061). statement
		// 단위로 보내면 각 statement가 autocommit으로 실행되어 CONCURRENTLY가 동작한다.
		for _, stmt := range splitSQLStatements(string(sql)) {
			if _, execErr := pool.Exec(ctx, stmt); execErr != nil {
				return fmt.Errorf("apply migrations: exec %s: %w", filename, execErr)
			}
		}
	}

	return nil
}

// splitSQLStatements는 SQL 텍스트를 top-level 세미콜론 기준으로 분할한다.
// dollar-quoted 블록($$ ... $$ 또는 $tag$ ... $tag$), 단일/이중 인용 문자열,
// 라인 주석(--), 블록 주석(/* */) 내부의 세미콜론은 구분자로 취급하지 않는다.
// 공백/주석만 남는 조각은 버린다(빈 Exec 방지).
func splitSQLStatements(sql string) []string {
	var (
		statements []string
		buf        strings.Builder
	)

	flush := func() {
		stmt := strings.TrimSpace(buf.String())
		buf.Reset()
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	runes := []rune(sql)
	for i := 0; i < len(runes); {
		c := runes[i]

		switch {
		case c == '-' && i+1 < len(runes) && runes[i+1] == '-':
			// 라인 주석: 개행 전까지 보존하며 그대로 통과(세미콜론 무시).
			for i < len(runes) && runes[i] != '\n' {
				buf.WriteRune(runes[i])
				i++
			}

		case c == '/' && i+1 < len(runes) && runes[i+1] == '*':
			// 블록 주석: */ 까지 통과.
			buf.WriteRune(runes[i])
			buf.WriteRune(runes[i+1])
			i += 2
			for i < len(runes) {
				if runes[i] == '*' && i+1 < len(runes) && runes[i+1] == '/' {
					buf.WriteRune(runes[i])
					buf.WriteRune(runes[i+1])
					i += 2
					break
				}
				buf.WriteRune(runes[i])
				i++
			}

		case c == '\'' || c == '"':
			// 인용 문자열/식별자: 닫는 동일 인용까지 통과. '' 또는 "" 이스케이프 처리.
			quote := c
			buf.WriteRune(runes[i])
			i++
			for i < len(runes) {
				buf.WriteRune(runes[i])
				if runes[i] == quote {
					if i+1 < len(runes) && runes[i+1] == quote {
						buf.WriteRune(runes[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}

		case c == '$':
			// dollar-quoting: $tag$ ... $tag$ (tag는 비거나 식별자).
			if tag, ok := dollarTag(runes, i); ok {
				buf.WriteString(tag)
				i += len([]rune(tag))
				for i < len(runes) {
					if other, ok2 := dollarTag(runes, i); ok2 && other == tag {
						buf.WriteString(other)
						i += len([]rune(other))
						break
					}
					buf.WriteRune(runes[i])
					i++
				}
			} else {
				buf.WriteRune(c)
				i++
			}

		case c == ';':
			flush()
			i++

		default:
			buf.WriteRune(c)
			i++
		}
	}

	flush()
	return statements
}

// dollarTag는 pos에서 시작하는 dollar-quote 태그($$ 또는 $tag$)를 인식해 그 문자열을 돌려준다.
// 태그가 아니면 ok=false.
func dollarTag(runes []rune, pos int) (string, bool) {
	if pos >= len(runes) || runes[pos] != '$' {
		return "", false
	}

	j := pos + 1
	for j < len(runes) {
		c := runes[j]
		if c == '$' {
			return string(runes[pos : j+1]), true
		}
		if !(c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", false
		}
		j++
	}

	return "", false
}

// resolveMigrationsDir는 migration 디렉터리 절대 경로를 결정한다.
func resolveMigrationsDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

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
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("malformed manifest line %q (want \"NNN filename.sql\")", line)
		}

		filenames = append(filenames, fields[len(fields)-1])
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan manifest: %w", scanErr)
	}

	if len(filenames) == 0 {
		return nil, fmt.Errorf("manifest %q has no entries", path)
	}

	return filenames, nil
}

package migrationrunner

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/dbmigrate"

	"github.com/kapu/hololive-api/scripts/migrations"
	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

type forbiddenBaselineContent struct {
	label   string
	pattern *regexp.Regexp
}

var (
	concurrentlyWordPattern   = regexp.MustCompile(`(?i)\bCONCURRENTLY\b`)
	forbiddenBaselineContents = []forbiddenBaselineContent{
		{"CREATE DATABASE", regexp.MustCompile(`(?i)\bCREATE\s+DATABASE\b`)},
		{`\connect`, regexp.MustCompile(`(?i)\\connect\b`)},
		{"CREATE SCHEMA public", regexp.MustCompile(`(?i)\bCREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?"?public"?`)},
		{"ALTER SCHEMA public OWNER", regexp.MustCompile(`(?i)\bALTER\s+SCHEMA\s+"?public"?\s+OWNER\b`)},
		{"OWNER TO", regexp.MustCompile(`(?i)\bOWNER\s+TO\b`)},
		{"SET ROLE", regexp.MustCompile(`(?i)\bSET\s+(?:LOCAL\s+|SESSION\s+)?ROLE\b`)},
		{"schema_migrations", regexp.MustCompile(`(?i)\bschema_migrations\b`)},
		{"schema_migration_checksums", regexp.MustCompile(`(?i)\bschema_migration_checksums\b`)},
		{"CONCURRENTLY", concurrentlyWordPattern},
	}
	migrationNumberPrefixPattern = regexp.MustCompile(`^\d+`)
	unguardedSetNotNullPattern   = regexp.MustCompile(`SET[^\S\n]+NOT[^\S\n]+NULL`)
	// 인덱스 규칙은 줄 단위라 DO 본문 안의 CREATE INDEX 줄도 잡는다.
	createIndexLinePattern             = regexp.MustCompile(`(?im)^[^\S\n]*CREATE[^\S\n]+(?:UNIQUE[^\S\n]+)?INDEX[^\S\n]+`)
	createIndexConcurrentlyLinePattern = regexp.MustCompile(`(?im)^[^\S\n]*CREATE[^\S\n]+(?:UNIQUE[^\S\n]+)?INDEX[^\S\n]+CONCURRENTLY[^\S\n]+`)
)

func TestEmbeddedMigrationsFollowConventions(t *testing.T) {
	if err := checkMigrationConventions(migrations.FS); err != nil {
		t.Fatal(err)
	}
}

func checkMigrationConventions(fsys fs.FS) error {
	entries, err := dbmigrate.Manifest(fsys)
	if err != nil {
		return fmt.Errorf("parse migration manifest: %w", err)
	}

	if ordinalErr := checkManifestOrdinals(fsys); ordinalErr != nil {
		return ordinalErr
	}

	sources, err := readMigrationSources(fsys)
	if err != nil {
		return err
	}

	files := slices.Sorted(maps.Keys(sources))
	if matchErr := checkManifestMatchesFiles(entries, files); matchErr != nil {
		return matchErr
	}

	if entries[0] != epoch3Baseline {
		return fmt.Errorf("epoch-3 manifest must begin with %s", epoch3Baseline)
	}

	errs := []error{checkMigrationNumberPrefixes(files)}
	for _, name := range files {
		errs = append(errs, checkMigrationSource(name, sources[name]))
	}

	return errors.Join(errs...)
}

// checkManifestOrdinals는 dbmigrate.Manifest가 보지 않는 001부터의 순서 토큰 연속성을 확인한다.
func checkManifestOrdinals(fsys fs.FS) error {
	content, err := fs.ReadFile(fsys, dbmigrate.ManifestName)
	if err != nil {
		return fmt.Errorf("read migration manifest: %w", err)
	}

	expected := 0

	for line := range strings.Lines(string(content)) {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}

		expected++

		if want := fmt.Sprintf("%03d", expected); fields[0] != want {
			return fmt.Errorf("migration manifest order drift: expected %s, got %s", want, fields[0])
		}
	}

	return nil
}

func readMigrationSources(fsys fs.FS) (map[string]string, error) {
	names, err := fs.Glob(fsys, "[0-9]*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migration files: %w", err)
	}

	sources := make(map[string]string, len(names))

	for _, name := range names {
		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}

		sources[name] = string(content)
	}

	return sources, nil
}

func checkManifestMatchesFiles(entries, files []string) error {
	var manifestOnly, sqlOnly []string

	for _, name := range entries {
		if !slices.Contains(files, name) {
			manifestOnly = append(manifestOnly, name)
		}
	}

	for _, name := range files {
		if !slices.Contains(entries, name) {
			sqlOnly = append(sqlOnly, name)
		}
	}

	if len(manifestOnly) == 0 && len(sqlOnly) == 0 {
		return nil
	}

	return fmt.Errorf("migration manifest and actual SQL files differ: manifest only %v, sql only %v", manifestOnly, sqlOnly)
}

func checkEpoch3Baseline(content string, segments []sqlsplit.Segment) error {
	for _, forbidden := range forbiddenBaselineContents {
		if forbidden.pattern.MatchString(content) {
			return fmt.Errorf("forbidden baseline content: %s", forbidden.label)
		}
	}

	if len(segments) != 1 || !segments[0].Transactional {
		return errors.New("epoch-3 baseline must be one top-level transaction")
	}

	return nil
}

func checkMigrationNumberPrefixes(files []string) error {
	seen := make(map[string]string, len(files))

	for _, name := range files {
		prefix := migrationNumberPrefixPattern.FindString(name)
		if previous, ok := seen[prefix]; ok {
			return fmt.Errorf("duplicate migration number prefix %s in %s and %s; use the last number + 1", prefix, previous, name)
		}

		seen[prefix] = name
	}

	return nil
}

func checkMigrationSource(name, content string) error {
	segments, err := sqlsplit.Segments(content)
	if err != nil {
		return fmt.Errorf("split %s: %w", name, err)
	}

	if name == epoch3Baseline {
		if baselineErr := checkEpoch3Baseline(content, segments); baselineErr != nil {
			return baselineErr
		}
	}

	// 유효한 CHECK를 먼저 만들어 SET NOT NULL의 전체 스캔을 피한다.
	if unguardedSetNotNullPattern.MatchString(content) &&
		(!strings.Contains(content, "NOT VALID") || !strings.Contains(content, "VALIDATE CONSTRAINT")) {
		return fmt.Errorf("%s uses SET NOT NULL without a preceding NOT VALID CHECK and VALIDATE CONSTRAINT", name)
	}

	if concurrentlyWordPattern.MatchString(content) {
		if count := executableStatementCount(content); count != 1 {
			return fmt.Errorf("%s uses CONCURRENTLY with %d SQL statements; keep CONCURRENTLY migrations single-statement", name, count)
		}
	}

	// 빈 DB에 적용하는 baseline만 트랜잭션 안에서 인덱스를 만든다.
	uncommented := stripMigrationComments(content)
	if name != epoch3Baseline && createIndexLinePattern.MatchString(uncommented) &&
		!createIndexConcurrentlyLinePattern.MatchString(uncommented) {
		return fmt.Errorf("%s creates a blocking index; use CREATE INDEX CONCURRENTLY in a single-statement migration", name)
	}

	return nil
}

// executableStatementCount는 sqlsplit이 문장으로 돌려주는 주석만 남은 조각을 세지 않는다.
func executableStatementCount(content string) int {
	count := 0

	for _, statement := range sqlsplit.Statements(content) {
		if strings.TrimSpace(stripMigrationComments(statement)) != "" {
			count++
		}
	}

	return count
}

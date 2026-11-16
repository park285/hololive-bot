package migrationrunner

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

var (
	createIndexConcurrentlyPattern = regexp.MustCompile(`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\b`)
	concurrentIndexTargetPattern   = regexp.MustCompile(`(?i)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"((?:""|[^"])*)"|([A-Za-z_][A-Za-z0-9_$]*))\s+ON\s+(?:ONLY\s+)?(?:(?:"((?:""|[^"])*)"|([A-Za-z_][A-Za-z0-9_$]*))\s*\.\s*)?(?:"((?:""|[^"])*)"|([A-Za-z_][A-Za-z0-9_$]*))(?:\s|\(|$)`)
)

type concurrentIndexTarget struct {
	indexName     string
	tableRelation string
}

func (e *guardedExecer) execFile(ctx context.Context, name, content string) error {
	targets, unparsed := concurrentIndexTargets(content)
	if unparsed {
		return fmt.Errorf("exec %s: concurrent index target identity could not be parsed", name)
	}

	segments, err := sqlsplit.Segments(content)
	if err != nil {
		return fmt.Errorf("exec %s: %w", name, err)
	}

	if err := e.execSegments(ctx, name, segments); err != nil {
		if joinErr := joinIndexCleanupError(ctx, e.conn, targets, err); joinErr != nil {
			return fmt.Errorf("join index cleanup error: %w", joinErr)
		}

		return nil
	}

	if err := dropInvalidIndexes(ctx, e.conn, targets); err != nil {
		return fmt.Errorf("drop invalid indexes: %w", err)
	}

	return nil
}

func (e *guardedExecer) execSegments(ctx context.Context, name string, segments []sqlsplit.Segment) error {
	for _, segment := range segments {
		if err := e.execSegment(ctx, name, segment); err != nil {
			return fmt.Errorf("exec segment: %w", err)
		}
	}

	return nil
}

func joinIndexCleanupError(ctx context.Context, conn *pgxpool.Conn, targets []concurrentIndexTarget, execErr error) error {
	cleanupErr := dropInvalidIndexes(ctx, conn, targets)
	if cleanupErr != nil {
		return errors.Join(execErr, cleanupErr)
	}

	return execErr
}

func concurrentIndexTargets(content string) ([]concurrentIndexTarget, bool) {
	statements := sqlsplit.Statements(content)
	targets := make([]concurrentIndexTarget, 0, len(statements))
	seen := make(map[concurrentIndexTarget]struct{}, len(statements))
	unparsed := false

	for _, statement := range statements {
		target, concurrent, parsed := parseConcurrentIndexStatement(statement)
		if !concurrent {
			continue
		}

		if !parsed {
			unparsed = true
			continue
		}

		if _, exists := seen[target]; exists {
			continue
		}

		seen[target] = struct{}{}
		targets = append(targets, target)
	}

	return targets, unparsed
}

func parseConcurrentIndexStatement(statement string) (concurrentIndexTarget, bool, bool) {
	statement = strings.TrimSpace(sqlsplit.StripComments(statement))

	location := createIndexConcurrentlyPattern.FindStringIndex(statement)

	if location == nil {
		return concurrentIndexTarget{}, false, true
	}

	if location[0] != 0 {
		return concurrentIndexTarget{}, false, true
	}

	match := concurrentIndexTargetPattern.FindStringSubmatch(statement)
	if match == nil {
		return concurrentIndexTarget{}, true, false
	}

	return buildConcurrentIndexTarget(match), true, true
}

func buildConcurrentIndexTarget(match []string) concurrentIndexTarget {
	indexName := parsedIdentifier(match[1], match[2])
	tableSchema := parsedIdentifier(match[3], match[4])
	tableName := parsedIdentifier(match[5], match[6])
	tableRelation := quotePostgresIdentifier(tableName)

	if tableSchema != "" {
		tableRelation = quotePostgresIdentifier(tableSchema) + "." + tableRelation
	}

	return concurrentIndexTarget{indexName: indexName, tableRelation: tableRelation}
}

func parsedIdentifier(quoted, unquoted string) string {
	if quoted != "" {
		return strings.ReplaceAll(quoted, `""`, `"`)
	}

	return strings.ToLower(unquoted)
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

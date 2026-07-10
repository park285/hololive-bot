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
	createIndexConcurrentlyPattern = regexp.MustCompile(mustPattern("create_index_concurrently.re"))
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
		return joinIndexCleanupError(ctx, e.conn, targets, err)
	}
	return dropInvalidIndexes(ctx, e.conn, targets)
}

func (e *guardedExecer) execSegments(ctx context.Context, name string, segments []sqlsplit.Segment) error {
	for _, segment := range segments {
		if err := e.execSegment(ctx, name, segment); err != nil {
			return err
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

func concurrentIndexNames(content string) []string {
	targets, _ := concurrentIndexTargets(content)
	names := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if _, exists := seen[target.indexName]; exists {
			continue
		}
		seen[target.indexName] = struct{}{}
		names = append(names, target.indexName)
	}
	return names
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
	statement = strings.TrimSpace(stripMigrationComments(statement))
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

func stripMigrationComments(statement string) string {
	var stripped strings.Builder
	stripped.Grow(len(statement))
	for pos := 0; pos < len(statement); {
		next, done := copyMigrationPart(&stripped, statement, pos)
		if done {
			break
		}
		pos = next
	}
	return stripped.String()
}

func copyMigrationPart(dst *strings.Builder, statement string, pos int) (int, bool) {
	if strings.HasPrefix(statement[pos:], "--") {
		return copyMigrationLineComment(dst, statement, pos)
	}
	if strings.HasPrefix(statement[pos:], "/*") {
		return copyMigrationBlockComment(dst, statement, pos)
	}
	if statement[pos] == '\'' || statement[pos] == '"' {
		return copyMigrationQuotedToken(dst, statement, pos), false
	}
	dst.WriteByte(statement[pos])
	return pos + 1, false
}

func copyMigrationLineComment(dst *strings.Builder, statement string, pos int) (int, bool) {
	newline := strings.IndexByte(statement[pos:], '\n')
	if newline < 0 {
		return len(statement), true
	}
	dst.WriteByte('\n')
	return pos + newline + 1, false
}

func copyMigrationBlockComment(dst *strings.Builder, statement string, pos int) (int, bool) {
	end, ok := migrationBlockCommentEnd(statement, pos)
	if !ok {
		return len(statement), true
	}
	dst.WriteByte(' ')
	return end, false
}

func migrationBlockCommentEnd(statement string, start int) (int, bool) {
	depth := 1
	pos := start + 2
	for {
		open := strings.Index(statement[pos:], "/*")
		closing := strings.Index(statement[pos:], "*/")
		if closing < 0 {
			return 0, false
		}
		if open >= 0 && open < closing {
			depth++
			pos += open + 2
			continue
		}
		depth--
		pos += closing + 2
		if depth == 0 {
			return pos, true
		}
	}
}

func copyMigrationQuotedToken(dst *strings.Builder, statement string, pos int) int {
	quote := statement[pos]
	dst.WriteByte(quote)
	pos++
	for pos < len(statement) {
		current := statement[pos]
		dst.WriteByte(current)
		pos++
		if isBackslashEscape(quote, current, pos, len(statement)) {
			dst.WriteByte(statement[pos])
			pos++
			continue
		}
		if current != quote {
			continue
		}
		if hasRepeatedQuote(statement, pos, quote) {
			dst.WriteByte(statement[pos])
			pos++
			continue
		}
		return pos
	}
	return pos
}

func isBackslashEscape(quote, current byte, pos, length int) bool {
	return quote == '\'' && current == '\\' && pos < length
}

func hasRepeatedQuote(statement string, pos int, quote byte) bool {
	return pos < len(statement) && statement[pos] == quote
}

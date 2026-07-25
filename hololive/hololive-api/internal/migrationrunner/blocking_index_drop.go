package migrationrunner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

var (
	dropIndexPattern             = regexp.MustCompile(`(?i)^DROP\s+INDEX\b`)
	dropIndexConcurrentlyPattern = regexp.MustCompile(`(?i)^DROP\s+INDEX\s+CONCURRENTLY\b`)
)

func configureBlockingIndexDropPolicy(ctx context.Context, conn *pgxpool.Conn, exec *guardedExecer, cfg Config) error {
	count, err := ledgerCount(ctx, conn)
	if err != nil {
		return err
	}

	exec.allowBlockingIndexDrop = count == 0 || cfg.AllowBlockingIndexDrop
	if count > 0 && cfg.AllowBlockingIndexDrop {
		cfg.logf("blocking index-removal override enabled for existing database; dedicated maintenance window is required")
	}
	return nil
}

func (e *guardedExecer) validateMigrationSource(name, content string) error {
	if e.allowBlockingIndexDrop {
		return nil
	}

	for _, statement := range sqlsplit.Statements(content) {
		normalized := strings.TrimSpace(stripMigrationComments(statement))
		if !dropIndexPattern.MatchString(normalized) || dropIndexConcurrentlyPattern.MatchString(normalized) {
			continue
		}
		return fmt.Errorf(
			"exec %s: blocking index removal is disabled on an existing database; use the concurrent form or rerun in a dedicated maintenance window with MIGRATION_ALLOW_BLOCKING_INDEX_DROP=true",
			name,
		)
	}
	return nil
}

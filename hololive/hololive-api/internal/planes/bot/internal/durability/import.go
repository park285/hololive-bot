package durability

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
)

type ImportTable string

const (
	ImportWebhookInbox         ImportTable = "bot_webhook_inbox"
	ImportWebhookHeads         ImportTable = "bot_webhook_heads"
	ImportCommandExecutions    ImportTable = "bot_command_executions"
	ImportReplyOutbox          ImportTable = "bot_reply_outbox"
	ImportReplyReplayAudit     ImportTable = "bot_reply_outbox_replay_audit"
	ImportReplyResolutionAudit ImportTable = "bot_reply_outbox_resolution_audit"
	ImportWebhookNonces        ImportTable = "bot_webhook_nonces"
)

type ImportSummary map[string]int64

func (s ImportSummary) Add(relation, bucket string, rows int64) {
	s[relation+"/"+bucket] = rows
}

var (
	ErrImportTargetNotEmpty  = errors.New("durability: import target already holds bot plane rows")
	ErrImportSummaryMismatch = errors.New("durability: imported rows differ from the source summary")
)

var (
	importSteps = []struct {
		table  ImportTable
		insert string
	}{
		{ImportWebhookInbox, mustSQL("import_webhook_inbox.sql")},
		{ImportWebhookHeads, mustSQL("import_webhook_heads.sql")},
		{ImportCommandExecutions, mustSQL("import_command_executions.sql")},
		{ImportReplyOutbox, mustSQL("import_reply_outbox.sql")},
		{ImportReplyReplayAudit, mustSQL("import_reply_replay_audit.sql")},
		{ImportReplyResolutionAudit, mustSQL("import_reply_resolution_audit.sql")},
		{ImportWebhookNonces, mustSQL("import_webhook_nonces.sql")},
	}
	importDiscardedReplySQL = mustSQL("import_reply_outbox_discarded.sql")
	importSummarySQL        = mustSQL("import_summary.sql")
)

func ImportTables() []ImportTable {
	tables := make([]ImportTable, 0, len(importSteps))

	for _, step := range importSteps {
		tables = append(tables, step.table)
	}

	return tables
}

func (s *Store) Import(ctx context.Context, rows map[ImportTable][][]any, source ImportSummary) error {
	if err := ensureStore(s); err != nil {
		return err
	}

	for _, step := range importSteps {
		if _, ok := rows[step.table]; !ok {
			return errors.Join(ErrInvalidArgument, fmt.Errorf("import rows are missing %s", step.table))
		}
	}

	if len(rows) != len(importSteps) {
		return errors.Join(ErrInvalidArgument, fmt.Errorf("import rows name %d tables, want %d", len(rows), len(importSteps)))
	}

	err := s.write(ctx, func(tx *sql.Tx, _ int64) error {
		existing, err := readImportSummary(ctx, tx)
		if err != nil {
			return err
		}

		for bucket, count := range existing {
			if count > 0 {
				return fmt.Errorf("%w: %s has %d rows", ErrImportTargetNotEmpty, bucket, count)
			}
		}

		for _, step := range importSteps {
			for _, row := range rows[step.table] {
				if _, execErr := tx.ExecContext(ctx, step.insert, row...); execErr != nil {
					return fmt.Errorf("insert %s row: %w", step.table, execErr)
				}
			}
		}

		if _, execErr := tx.ExecContext(ctx, importDiscardedReplySQL); execErr != nil {
			return fmt.Errorf("restore discarded reply rows: %w", execErr)
		}

		imported, err := readImportSummary(ctx, tx)
		if err != nil {
			return err
		}

		if !maps.Equal(imported, source) {
			return fmt.Errorf("%w: source %v, imported %v", ErrImportSummaryMismatch, source, imported)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("import bot plane rows: %w", err)
	}

	return nil
}

func readImportSummary(ctx context.Context, tx *sql.Tx) (ImportSummary, error) {
	rows, err := tx.QueryContext(ctx, importSummarySQL)
	if err != nil {
		return nil, fmt.Errorf("select import summary: %w", err)
	}
	defer rows.Close()

	summary := ImportSummary{}

	for rows.Next() {
		var (
			relation, bucket string
			count            int64
		)

		if err := rows.Scan(&relation, &bucket, &count); err != nil {
			return nil, fmt.Errorf("scan import summary: %w", err)
		}

		summary.Add(relation, bucket, count)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read import summary: %w", err)
	}

	return summary, nil
}

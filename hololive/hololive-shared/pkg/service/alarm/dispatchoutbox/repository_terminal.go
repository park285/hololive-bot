package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
)

func (r *PgxRepository) terminalUpdates(ctx context.Context, updates []TerminalUpdate, status Status, workerID string) error {
	if len(updates) == 0 {
		return nil
	}

	column, statusFilter := terminalStatusSQL(status)

	raw, err := jsonv2.Marshal(updates)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: marshal batch: %w", err)
	}

	query := fmt.Sprintf(mustSQL("repository_terminal_0019_01.sql"), column, statusFilter)

	tag, err := r.pool.Exec(ctx, query, jsonbRecordsetParam(raw), string(status), workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: %w", err)
	}

	if err := expectRowsAffected(tag.RowsAffected(), len(updates), "mark dispatch deliveries terminal"); err != nil {
		return fmt.Errorf("expect rows affected: %w", err)
	}

	return nil
}

func terminalStatusSQL(status Status) (statusColumn, timestampColumn string) {
	overrides := map[Status][2]string{
		StatusDLQ:         {"dlq_at", "status IN ('leased','sending')"},
		StatusQuarantined: {"quarantined_at", "status = 'sending'"},
		StatusCancelled:   {"cancelled_at", terminalNonFinalStatusFilter}, //nolint:misspell // 실제 DB 컬럼명이라, canceled로 바꾸면 SQL이 깨진다.
	}
	if sql, ok := overrides[status]; ok {
		return sql[0], sql[1]
	}

	return "sent_at", terminalNonFinalStatusFilter
}

const terminalNonFinalStatusFilter = "status NOT IN ('sent','dlq','quarantined','cancelled')"

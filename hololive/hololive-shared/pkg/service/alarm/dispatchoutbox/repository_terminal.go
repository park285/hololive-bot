package dispatchoutbox

import (
	"context"
	"fmt"

	json "github.com/park285/shared-go/pkg/json"
)

func (r *PgxRepository) terminalUpdates(ctx context.Context, updates []TerminalUpdate, status Status, workerID string) error {
	if len(updates) == 0 {
		return nil
	}
	column, statusFilter := terminalStatusSQL(status)
	raw, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: marshal batch: %w", err)
	}
	query := fmt.Sprintf(mustSQL("repository_terminal_0019_01.sql"), column, statusFilter)
	tag, err := r.pool.Exec(ctx, query, jsonbRecordsetParam(raw), string(status), workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(updates), "mark dispatch deliveries terminal")
}

func terminalStatusSQL(status Status) (statusColumn, timestampColumn string) {
	overrides := map[Status][2]string{
		StatusDLQ:         {"dlq_at", "status = 'leased'"},
		StatusQuarantined: {"quarantined_at", "status = 'sending'"},
		StatusCancelled:   {"cancelled_at", terminalNonFinalStatusFilter},
	}
	if sql, ok := overrides[status]; ok {
		return sql[0], sql[1]
	}
	return "sent_at", terminalNonFinalStatusFilter
}

const terminalNonFinalStatusFilter = "status NOT IN ('sent','dlq','quarantined','cancelled')"

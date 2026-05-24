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
	query := fmt.Sprintf(`
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS x(id BIGINT, error TEXT)
		)
		UPDATE alarm_dispatch_deliveries d
		SET status=$2,
			%s=NOW(),
			locked_by=NULL,
			locked_at=NULL,
			lock_expires_at=NULL,
			last_error=CASE WHEN input.error = '' THEN d.last_error ELSE input.error END,
			updated_at=NOW()
		FROM input
		WHERE d.id = input.id
		  AND d.locked_by = $3
		  AND %s`, column, statusFilter)
	tag, err := r.pool.Exec(ctx, query, jsonbRecordsetParam(raw), string(status), workerID)
	if err != nil {
		return fmt.Errorf("mark dispatch deliveries terminal: %w", err)
	}
	return expectRowsAffected(tag.RowsAffected(), len(updates), "mark dispatch deliveries terminal")
}

func terminalStatusSQL(status Status) (string, string) {
	switch status {
	case StatusDLQ:
		return "dlq_at", "status = 'leased'"
	case StatusQuarantined:
		return "quarantined_at", "status = 'sending'"
	case StatusCancelled:
		return "cancelled_at", "status NOT IN ('sent','dlq','quarantined','cancelled')"
	default:
		return "sent_at", "status NOT IN ('sent','dlq','quarantined','cancelled')"
	}
}

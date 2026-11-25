package dispatchoutbox

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"
)

func leaseMilliseconds(d time.Duration) (int64, error) {
	if d < time.Millisecond {
		return 0, fmt.Errorf("duration must be at least 1ms: %s", d)
	}

	return d.Milliseconds(), nil
}

func (r *PgxRepository) MarkSent(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, irisRequestID string, firstCheck time.Duration) error {
	if len(claims) == 0 {
		return nil
	}

	if irisRequestID == "" {
		return fmt.Errorf("%s: iris request id is required", fenceActionMarkSent)
	}

	firstCheckMS, err := leaseMilliseconds(firstCheck)
	if err != nil {
		return fmt.Errorf("%s: first handoff check: %w", fenceActionMarkSent, err)
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return fmt.Errorf("%s: marshal claims: %w", fenceActionMarkSent, err)
	}

	return r.applyFenced(ctx, r.pool, fenceActionMarkSent, markSentSQL, fencedTargetIDs(claims),
		jsonbRecordsetParam(raw), workerID, unitID, irisRequestID, firstCheckMS)
}

func (r *PgxRepository) ReleaseLeased(ctx context.Context, claims []DeliveryClaim, workerID string) error {
	return r.applyClaims(ctx, fenceActionReleaseLeased, releaseLeasedSQL, claims, workerID)
}

func (r *PgxRepository) applyClaims(ctx context.Context, action string, query pgxdb.Statement, claims []DeliveryClaim, workerID string) error {
	if len(claims) == 0 {
		return nil
	}

	raw, err := jsonv2.Marshal(claims)
	if err != nil {
		return fmt.Errorf("%s: marshal claims: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(claims), jsonbRecordsetParam(raw), workerID)
}

func (r *PgxRepository) RouteFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, routeFailuresSQL, fenceActionRouteFailures)
}

func (r *PgxRepository) RouteSendingFailures(ctx context.Context, updates []FailureUpdate, workerID string) error {
	return r.routeFailureUpdates(ctx, updates, workerID, routeSendingFailuresSQL, fenceActionRouteSendingFailures)
}

func (r *PgxRepository) RequeuePreSend(ctx context.Context, updates []FailureUpdate, workerID string) error {
	for i := range updates {
		if updates[i].TargetStatus != StatusRetry {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", fenceActionRequeuePreSend, updates[i].TargetStatus, updates[i].ID)
		}
	}

	return r.routeFailureUpdates(ctx, updates, workerID, requeuePreSendSQL, fenceActionRequeuePreSend)
}

func (r *PgxRepository) routeFailureUpdates(ctx context.Context, updates []FailureUpdate, workerID string, query pgxdb.Statement, action string) error {
	if len(updates) == 0 {
		return nil
	}

	if err := validateFailureUpdates(updates, action); err != nil {
		return err
	}

	raw, err := jsonv2.Marshal(prepareFailureUpdates(updates))
	if err != nil {
		return fmt.Errorf("%s: marshal batch: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(updates), jsonbRecordsetParam(raw), workerID)
}

func validateFailureUpdates(updates []FailureUpdate, action string) error {
	seen := make(map[int64]struct{}, len(updates))
	for i := range updates {
		if _, duplicate := seen[updates[i].ID]; duplicate {
			return fmt.Errorf("%s: duplicate delivery %d", action, updates[i].ID)
		}

		seen[updates[i].ID] = struct{}{}
		if updates[i].TargetStatus != StatusRetry && updates[i].TargetStatus != StatusDLQ {
			return fmt.Errorf("%s: unsupported target status %q for delivery %d", action, updates[i].TargetStatus, updates[i].ID)
		}

		if updates[i].RetryAfter < 0 {
			return fmt.Errorf("%s: negative retry delay %s for delivery %d", action, updates[i].RetryAfter, updates[i].ID)
		}
	}

	return nil
}

type failureRouteInput struct {
	*FailureUpdate

	RetryAfterMS *int64 `json:"retry_after_ms,omitempty"`
	MarkDLQ      bool   `json:"mark_dlq,omitzero"`
}

func prepareFailureUpdates(updates []FailureUpdate) []failureRouteInput {
	inputs := make([]failureRouteInput, len(updates))
	for i := range updates {
		update := &updates[i]

		inputs[i] = failureRouteInput{FailureUpdate: update, MarkDLQ: update.TargetStatus == StatusDLQ}

		if update.TargetStatus == StatusRetry {
			inputs[i].RetryAfterMS = new(update.RetryAfter.Milliseconds())
		}
	}

	return inputs
}

func (r *PgxRepository) MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, fenceActionMoveToDLQ, repositoryTerminalDLQSQL, updates, workerID)
}

func (r *PgxRepository) Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error {
	return r.terminalUpdates(ctx, fenceActionQuarantine, repositoryTerminalQuarantineSQL, updates, workerID)
}

func (r *PgxRepository) terminalUpdates(ctx context.Context, action string, query pgxdb.Statement, updates []TerminalUpdate, workerID string) error {
	if len(updates) == 0 {
		return nil
	}

	raw, err := jsonv2.Marshal(updates)
	if err != nil {
		return fmt.Errorf("%s: marshal batch: %w", action, err)
	}

	return r.applyFenced(ctx, r.pool, action, query, fencedTargetIDs(updates), jsonbRecordsetParam(raw), workerID)
}

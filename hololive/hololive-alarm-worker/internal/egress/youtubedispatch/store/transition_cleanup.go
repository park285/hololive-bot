package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type CleanupResult struct {
	DeletedOutboxes  int
	GuardedOutboxes  int
	ExaminedOutboxes int
	NextCursor       CleanupCursor
	Guards           map[CleanupGuardReason]int
}

type CleanupGuardReason string

const (
	CleanupGuardLedgerMissing      CleanupGuardReason = "ledger_missing"
	CleanupGuardLedgerMismatch     CleanupGuardReason = "ledger_mismatch"
	CleanupGuardActiveLogicalGroup CleanupGuardReason = "active_logical_group"
)

// CleanupCursor advances a fixed-cutoff cleanup scan past guarded outboxes so
// one old logical group cannot starve later eligible rows.
type CleanupCursor struct {
	TerminalAt time.Time
	OutboxID   int64
}

type cleanupCandidate struct {
	ID         int64               `db:"id"`
	Status     domain.OutboxStatus `db:"status"`
	TerminalAt time.Time           `db:"terminal_at"`
}

type cleanupBounds struct {
	cutoff          time.Time
	afterTerminalAt *time.Time
}

func (s *TransitionStore) CleanupTerminalOutboxes(
	ctx context.Context,
	cutoff time.Time,
	after CleanupCursor,
	limit int,
) (CleanupResult, error) {
	if err := s.ensureReady(ctx); err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: %w", err)
	}

	bounds, err := canonicalCleanupBounds(cutoff, after, limit)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: canonical bounds: %w", err)
	}

	var result CleanupResult

	err = s.executeTx(ctx, "cleanup terminal outboxes", func(tx dbx.Querier) error {
		var cleanupErr error

		result, cleanupErr = s.cleanupTerminalTx(ctx, tx, bounds.cutoff, bounds.afterTerminalAt, after.OutboxID, limit)
		if cleanupErr != nil {
			return fmt.Errorf("cleanup terminal outboxes: transaction body: %w", cleanupErr)
		}

		return nil
	})
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: execute transaction: %w", err)
	}

	return result, nil
}

func canonicalCleanupBounds(
	cutoff time.Time,
	after CleanupCursor,
	limit int,
) (cleanupBounds, error) {
	canonicalCutoff, err := lifecycle.CanonicalTime(cutoff)
	if err != nil {
		return cleanupBounds{}, fmt.Errorf("cleanup terminal outboxes: cutoff: %w", err)
	}

	if limit <= 0 {
		return cleanupBounds{}, errors.New("cleanup terminal outboxes: limit must be positive")
	}

	if after.OutboxID < 0 {
		return cleanupBounds{}, errors.New("cleanup terminal outboxes: cursor outbox id must be nonnegative")
	}

	if after.TerminalAt.IsZero() {
		return cleanupBounds{cutoff: canonicalCutoff}, nil
	}

	canonicalAfter, err := lifecycle.CanonicalTime(after.TerminalAt)
	if err != nil {
		return cleanupBounds{}, fmt.Errorf("cleanup terminal outboxes: cursor terminal at: %w", err)
	}

	return cleanupBounds{cutoff: canonicalCutoff, afterTerminalAt: &canonicalAfter}, nil
}

func (s *TransitionStore) cleanupTerminalTx(
	ctx context.Context,
	tx dbx.Querier,
	cutoff time.Time,
	afterTerminalAt *time.Time,
	afterOutboxID int64,
	limit int,
) (CleanupResult, error) {
	candidates, err := loadCleanupCandidates(ctx, tx, cutoff, afterTerminalAt, afterOutboxID, limit)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: load candidates: %w", err)
	}

	if len(candidates) == 0 {
		return CleanupResult{}, nil
	}

	result := cleanupCandidateResult(candidates)

	children, err := loadCleanupChildren(ctx, tx, cleanupCandidateIDs(candidates))
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: load children: %w", err)
	}

	eligible, guards, err := s.cleanupEligibleOutboxes(ctx, tx, candidates, children)
	if err != nil {
		return CleanupResult{}, fmt.Errorf("cleanup terminal outboxes: evaluate eligibility: %w", err)
	}

	result.Guards = guards
	result.GuardedOutboxes = len(candidates) - len(eligible)

	if len(eligible) == 0 {
		return result, nil
	}

	result.DeletedOutboxes, err = deleteCleanupOutboxes(ctx, tx, eligible, cutoff)
	if err != nil {
		return result, fmt.Errorf("cleanup terminal outboxes: delete eligible rows: %w", err)
	}

	return result, nil
}

func cleanupCandidateResult(candidates []cleanupCandidate) CleanupResult {
	last := candidates[len(candidates)-1]

	return CleanupResult{
		ExaminedOutboxes: len(candidates),
		NextCursor:       CleanupCursor{TerminalAt: last.TerminalAt, OutboxID: last.ID},
	}
}

func cleanupCandidateIDs(candidates []cleanupCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for i := range candidates {
		ids = append(ids, candidates[i].ID)
	}

	return ids
}

func deleteCleanupOutboxes(
	ctx context.Context,
	tx dbx.Querier,
	eligible []int64,
	cutoff time.Time,
) (int, error) {
	rows, err := tx.Query(
		ctx,
		mustSQL("transition_cleanup_delete.sql"),
		eligible,
		[]string{string(domain.OutboxStatusSent), string(domain.OutboxStatusFailed)},
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup terminal outboxes: delete: %w", err)
	}
	defer rows.Close()

	deleted, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, fmt.Errorf("cleanup terminal outboxes: collect deleted ids: %w", err)
	}

	if len(deleted) != len(eligible) {
		return 0, &transitionConflictError{
			operation: "cleanup terminal outboxes",
			detail:    fmt.Sprintf("deleted count %d does not match eligible count %d", len(deleted), len(eligible)),
		}
	}

	return len(deleted), nil
}

func (s *TransitionStore) CleanupExpiredFanoutOutboxes(
	ctx context.Context,
	createdBefore time.Time,
	lockExpiredBefore time.Time,
	limit int,
) (int, error) {
	if err := s.ensureReady(ctx); err != nil {
		return 0, fmt.Errorf("cleanup expired fanout outboxes: %w", err)
	}

	canonicalCreatedBefore, err := lifecycle.CanonicalTime(createdBefore)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired fanout outboxes: created before: %w", err)
	}

	canonicalLockExpiry, err := lifecycle.CanonicalTime(lockExpiredBefore)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired fanout outboxes: lock expiry: %w", err)
	}

	if limit <= 0 {
		return 0, errors.New("cleanup expired fanout outboxes: limit must be positive")
	}

	rows, err := s.db.Query(
		ctx,
		mustSQL("fanout_cleanup_expired.sql"),
		domain.OutboxStatusPending,
		canonicalCreatedBefore,
		canonicalLockExpiry,
		limit,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired fanout outboxes: delete: %w", err)
	}
	defer rows.Close()

	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return 0, fmt.Errorf("cleanup expired fanout outboxes: collect: %w", err)
	}

	return len(ids), nil
}

func loadCleanupCandidates(
	ctx context.Context,
	db dbx.Querier,
	cutoff time.Time,
	afterTerminalAt *time.Time,
	afterOutboxID int64,
	limit int,
) ([]cleanupCandidate, error) {
	var candidates []cleanupCandidate

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&candidates,
		"load terminal outbox cleanup candidates",
		mustSQL("transition_cleanup_candidates.sql"),
		[]string{string(domain.OutboxStatusSent), string(domain.OutboxStatusFailed)},
		cutoff,
		afterTerminalAt,
		afterOutboxID,
		limit,
	); err != nil {
		return nil, fmt.Errorf("load terminal outbox cleanup candidates: %w", err)
	}

	return candidates, nil
}

func loadCleanupChildren(ctx context.Context, db dbx.Querier, outboxIDs []int64) ([]transitionRow, error) {
	var rows []transitionRow

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&rows,
		"load terminal outbox cleanup children",
		mustSQL("transition_cleanup_children.sql"),
		outboxIDs,
	); err != nil {
		return nil, fmt.Errorf("load terminal outbox cleanup children: %w", err)
	}

	return rows, nil
}

func (s *TransitionStore) cleanupEligibleOutboxes(
	ctx context.Context,
	db dbx.Querier,
	candidates []cleanupCandidate,
	children []transitionRow,
) ([]int64, map[CleanupGuardReason]int, error) {
	childrenByOutbox, requested, keys, err := cleanupLogicalInputs(children, len(candidates))
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup terminal outboxes: build logical inputs: %w", err)
	}

	ledger, _, err := loadTransitionLedger(ctx, db, keys)
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup terminal outboxes: load ledger: %w", err)
	}

	siblings, err := s.loadLogicalGroupRows(ctx, db, requested)
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup terminal outboxes: load siblings: %w", err)
	}

	eligible := make([]int64, 0, len(candidates))
	guards := make(map[CleanupGuardReason]int)

	for i := range candidates {
		guard, guardErr := cleanupCandidateGuard(childrenByOutbox[candidates[i].ID], ledger, siblings)
		if guardErr != nil {
			return nil, nil, fmt.Errorf("cleanup terminal outboxes: evaluate candidate %d: %w", candidates[i].ID, guardErr)
		}

		if guard == "" {
			eligible = append(eligible, candidates[i].ID)
		} else {
			guards[guard]++
		}
	}

	return eligible, guards, nil
}

func cleanupLogicalInputs(
	children []transitionRow,
	candidateCount int,
) (map[int64][]transitionRow, []requestedLogicalGroup, []ytcontentid.LogicalKey, error) {
	childrenByOutbox := make(map[int64][]transitionRow, candidateCount)
	requested := make([]requestedLogicalGroup, 0, len(children))
	keys := make([]ytcontentid.LogicalKey, 0, len(children))

	for i := range children {
		key, err := children[i].logicalKey()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("cleanup terminal outboxes: child identity: %w", err)
		}

		outbox := children[i].domainOutbox()

		requested = append(requested, requestedLogicalGroup{
			key: key, delivery: children[i].domainDelivery(), outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
		keys = append(keys, key)
		childrenByOutbox[children[i].OutboxID] = append(childrenByOutbox[children[i].OutboxID], children[i])
	}

	return childrenByOutbox, requested, uniqueLogicalKeys(keys), nil
}

func cleanupCandidateGuard(
	children []transitionRow,
	ledger map[ytcontentid.LogicalKey]DeliveryLedgerRecord,
	siblings []transitionRow,
) (CleanupGuardReason, error) {
	for i := range children {
		key, err := children[i].logicalKey()
		if err != nil {
			return "", fmt.Errorf("child identity: %w", err)
		}

		record, ok := ledger[key]

		if !ok {
			return CleanupGuardLedgerMissing, nil
		}

		if !cleanupLedgerSupportsChild(record, children[i].Status) {
			return CleanupGuardLedgerMismatch, nil
		}

		if activeSiblingOutsideOutbox(siblings, key, children[i].OutboxID) {
			return CleanupGuardActiveLogicalGroup, nil
		}
	}

	return "", nil
}

func cleanupLedgerSupportsChild(record DeliveryLedgerRecord, status lifecycle.DeliveryStatus) bool {
	switch status {
	case lifecycle.StatusSent:
		return record.Status == LedgerStatusSent && record.SentAt != nil
	case lifecycle.StatusQuarantined:
		return record.Status == LedgerStatusQuarantined || record.Status == LedgerStatusSent
	case lifecycle.StatusPending, lifecycle.StatusSending, lifecycle.StatusFailed:
		return false
	}

	return false
}

func activeSiblingOutsideOutbox(rows []transitionRow, key ytcontentid.LogicalKey, outboxID int64) bool {
	for i := range rows {
		rowKey, err := rows[i].logicalKey()
		if err != nil || rowKey != key || rows[i].OutboxID == outboxID {
			continue
		}

		if rows[i].Status == lifecycle.StatusPending || rows[i].Status == lifecycle.StatusSending {
			return true
		}
	}

	return false
}

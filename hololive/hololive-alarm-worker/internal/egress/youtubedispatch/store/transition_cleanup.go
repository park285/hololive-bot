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

	CleanupGuardIncompleteLogicalGroup CleanupGuardReason = "incomplete_logical_group"
)

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
		transitionCleanupDeleteSQL,
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
	pendingAge time.Duration,
	limit int,
) (int, error) {
	if pendingAge <= 0 {
		return 0, errors.New("cleanup expired fanout outboxes: pending age must be positive")
	}

	if limit <= 0 {
		return 0, errors.New("cleanup expired fanout outboxes: limit must be positive")
	}

	rows, err := s.db.Query(
		ctx,
		fanoutCleanupExpiredSQL,
		domain.OutboxStatusPending,
		pendingAge.Milliseconds(),
		s.config.LockTimeout.Milliseconds(),
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

	if err := dbx.SelectSQL(
		ctx,
		db,
		&candidates,
		"load terminal outbox cleanup candidates",
		transitionCleanupCandidatesSQL,
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

type cleanupObservedRow struct {
	transitionRow

	Locked bool
}

func loadCleanupChildren(ctx context.Context, db dbx.Querier, outboxIDs []int64) ([]cleanupObservedRow, error) {
	var rows []cleanupObservedRow

	if err := dbx.SelectSQL(
		ctx,
		db,
		&rows,
		"load terminal outbox cleanup children",
		transitionCleanupChildrenSQL,
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
	children []cleanupObservedRow,
) ([]int64, map[CleanupGuardReason]int, error) {
	inputs, err := cleanupLogicalInputs(children, len(candidates))
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup terminal outboxes: build logical inputs: %w", err)
	}

	var (
		siblings   []transitionRow
		incomplete map[ytcontentid.LogicalKey]struct{}
	)

	if len(inputs.requested) > 0 {
		siblings, incomplete, err = s.loadCleanupSiblings(ctx, db, inputs.requested)
		if err != nil {
			return nil, nil, fmt.Errorf("cleanup terminal outboxes: load siblings: %w", err)
		}
	}

	guards := make(map[CleanupGuardReason]int)
	covered := make([]int64, 0, len(candidates))
	keys := make([]ytcontentid.LogicalKey, 0, len(inputs.requested))

	for i := range candidates {
		if !inputs.covers(candidates[i].ID, incomplete) {
			guards[CleanupGuardIncompleteLogicalGroup]++

			continue
		}

		covered = append(covered, candidates[i].ID)
		keys = append(keys, inputs.keysByOutbox[candidates[i].ID]...)
	}

	ledger, _, err := loadTransitionLedger(ctx, db, uniqueLogicalKeys(keys))
	if err != nil {
		return nil, nil, fmt.Errorf("cleanup terminal outboxes: load ledger: %w", err)
	}

	eligible := make([]int64, 0, len(covered))

	for _, outboxID := range covered {
		guard, guardErr := cleanupCandidateGuard(inputs.childrenByOutbox[outboxID], ledger, siblings)
		if guardErr != nil {
			return nil, nil, fmt.Errorf("cleanup terminal outboxes: evaluate candidate %d: %w", outboxID, guardErr)
		}

		if guard == "" {
			eligible = append(eligible, outboxID)
		} else {
			guards[guard]++
		}
	}

	return eligible, guards, nil
}

type cleanupInputs struct {
	childrenByOutbox map[int64][]transitionRow
	keysByOutbox     map[int64][]ytcontentid.LogicalKey
	uncovered        map[int64]struct{}
	requested        []requestedLogicalGroup
}

func cleanupLogicalInputs(children []cleanupObservedRow, candidateCount int) (cleanupInputs, error) {
	inputs := cleanupInputs{
		childrenByOutbox: make(map[int64][]transitionRow, candidateCount),
		keysByOutbox:     make(map[int64][]ytcontentid.LogicalKey, candidateCount),
		uncovered:        make(map[int64]struct{}),
		requested:        make([]requestedLogicalGroup, 0, len(children)),
	}

	for i := range children {
		if !children[i].Locked {
			inputs.uncovered[children[i].OutboxID] = struct{}{}
		}
	}

	for i := range children {
		child := children[i].transitionRow
		if _, skipped := inputs.uncovered[child.OutboxID]; skipped {
			continue
		}

		key, err := child.logicalKey()
		if err != nil {
			return cleanupInputs{}, fmt.Errorf("cleanup terminal outboxes: child identity: %w", err)
		}

		outbox := child.domainOutbox()

		inputs.requested = append(inputs.requested, requestedLogicalGroup{
			key: key, delivery: child.domainDelivery(), outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
		inputs.childrenByOutbox[child.OutboxID] = append(inputs.childrenByOutbox[child.OutboxID], child)
		inputs.keysByOutbox[child.OutboxID] = append(inputs.keysByOutbox[child.OutboxID], key)
	}

	return inputs, nil
}

func (inputs cleanupInputs) covers(outboxID int64, incomplete map[ytcontentid.LogicalKey]struct{}) bool {
	if _, skipped := inputs.uncovered[outboxID]; skipped {
		return false
	}

	for _, key := range inputs.keysByOutbox[outboxID] {
		if _, ok := incomplete[key]; ok {
			return false
		}
	}

	return true
}

type cleanupGroupRow struct {
	cleanupObservedRow

	SaturatedKeys []int64
}

type cleanupIdentityRequest struct {
	keyNo     int64
	candidate string
}

type cleanupGroupArgs struct {
	ids        []int64
	roomIDs    []string
	kinds      []string
	candidates []string
	keyNos     []int64
	keys       []ytcontentid.LogicalKey
	keyNoByKey map[ytcontentid.LogicalKey]int64
}

func newCleanupGroupArgs(requested []requestedLogicalGroup) cleanupGroupArgs {
	args := cleanupGroupArgs{
		ids:        make([]int64, 0, len(requested)),
		roomIDs:    make([]string, 0, len(requested)*3),
		kinds:      make([]string, 0, len(requested)*3),
		candidates: make([]string, 0, len(requested)*3),
		keyNos:     make([]int64, 0, len(requested)*3),
		keys:       make([]ytcontentid.LogicalKey, 0, len(requested)),
		keyNoByKey: make(map[ytcontentid.LogicalKey]int64, len(requested)),
	}
	seen := make(map[cleanupIdentityRequest]struct{}, len(requested)*3)

	for i := range requested {
		key := requested[i].key

		args.ids = append(args.ids, requested[i].delivery.ID)

		keyNo, ok := args.keyNoByKey[key]
		if !ok {
			args.keys = append(args.keys, key)
			keyNo = int64(len(args.keys))
			args.keyNoByKey[key] = keyNo
		}

		for _, candidate := range requested[i].candidate {
			request := cleanupIdentityRequest{keyNo: keyNo, candidate: candidate}
			if _, dup := seen[request]; dup {
				continue
			}

			seen[request] = struct{}{}

			args.roomIDs = append(args.roomIDs, key.RoomID)
			args.kinds = append(args.kinds, string(key.Kind))
			args.candidates = append(args.candidates, candidate)
			args.keyNos = append(args.keyNos, keyNo)
		}
	}

	return args
}

func (s *TransitionStore) loadCleanupSiblings(
	ctx context.Context,
	db dbx.Querier,
	requested []requestedLogicalGroup,
) ([]transitionRow, map[ytcontentid.LogicalKey]struct{}, error) {
	args := newCleanupGroupArgs(requested)

	var observed []cleanupGroupRow

	if err := dbx.SelectSQL(
		ctx,
		db,
		&observed,
		"load terminal outbox cleanup siblings",
		transitionCleanupGroupRowsSQL,
		args.ids,
		args.roomIDs,
		args.kinds,
		args.candidates,
		args.keyNos,
		s.config.LogicalGroupLimit+1,
	); err != nil {
		return nil, nil, fmt.Errorf("load terminal outbox cleanup siblings: %w", err)
	}

	incomplete := make(map[ytcontentid.LogicalKey]struct{})
	counts := make(map[ytcontentid.LogicalKey]int, len(args.keys))
	siblings := make([]transitionRow, 0, len(observed))

	for i := range observed {
		for _, keyNo := range observed[i].SaturatedKeys {
			if keyNo < 1 || keyNo > int64(len(args.keys)) {
				return nil, nil, fmt.Errorf("load terminal outbox cleanup siblings: key number %d out of range", keyNo)
			}

			incomplete[args.keys[keyNo-1]] = struct{}{}
		}

		key, err := observed[i].logicalKey()
		if err != nil {
			continue
		}

		if _, ok := args.keyNoByKey[key]; !ok {
			continue
		}

		counts[key]++
		if !observed[i].Locked || counts[key] > s.config.LogicalGroupLimit {
			incomplete[key] = struct{}{}

			continue
		}

		siblings = append(siblings, observed[i].transitionRow)
	}

	return siblings, incomplete, nil
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

		if children[i].Status == lifecycle.StatusFailed {
			if activeSiblingOutsideOutbox(siblings, key, children[i].OutboxID) {
				return CleanupGuardActiveLogicalGroup, nil
			}

			continue
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

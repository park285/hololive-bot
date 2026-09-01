package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const staleSendingQuarantineReason = "stale sending; external send outcome unknown"

type QuarantineResult struct {
	ApplyResult

	QuarantinedDeliveries int
	Blocked               []BlockedLogicalGroup
}

func (s *TransitionStore) QuarantineStaleLogicalGroups(ctx context.Context, limit int) (QuarantineResult, error) {
	if err := s.ensureReady(ctx); err != nil {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, fmt.Errorf("quarantine stale logical groups: %w", err)
	}

	if limit <= 0 {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, errors.New("quarantine stale logical groups: limit must be positive")
	}

	at, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, fmt.Errorf("quarantine stale logical groups: at: %w", err)
	}

	cutoff := at.Add(-s.config.LockTimeout)

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		result, transitions, writes, applyErr := s.quarantineStaleOnce(ctx, at, cutoff, limit)
		if applyErr == nil {
			result.CommitAdjudication = priorAdjudication

			return result, nil
		}

		decision := s.adjudicateRowApply(
			ctx,
			"quarantine logical groups",
			"logical groups contain mixed pre-state and QUARANTINED post-state",
			transitions,
			nil,
			applyErr,
			priorAdjudication,
			attempt,
		)
		if decision.retry {
			priorAdjudication = decision.prior

			continue
		}

		result.ApplyResult = decision.result
		if decision.result.Outcome == ApplyApplied {
			confirmed, err := confirmQuarantinePostState(ctx, s.db, writes, applyErr, result)
			if err != nil {
				return confirmed, fmt.Errorf("quarantine stale logical groups: confirm post-state: %w", err)
			}

			return confirmed, nil
		}

		return result, decision.err
	}

	return QuarantineResult{ApplyResult: newApplyResult(ApplyIndeterminate, nil)}, errors.New("quarantine stale logical groups: retry loop exhausted")
}

func confirmQuarantinePostState(
	ctx context.Context,
	db dbx.Querier,
	writes []LedgerWrite,
	applyErr error,
	result QuarantineResult,
) (QuarantineResult, error) {
	if err := validateQuarantineLedger(ctx, db, writes); err != nil {
		result.ApplyResult = adjudicated(newApplyResult(ApplyIndeterminate, nil), applyErr, CommitMixed)

		return result, errors.Join(applyErr, err)
	}

	return result, nil
}

func (s *TransitionStore) quarantineStaleOnce(
	ctx context.Context,
	at time.Time,
	cutoff time.Time,
	limit int,
) (QuarantineResult, []rowTransition, []LedgerWrite, error) {
	var (
		result      QuarantineResult
		transitions []rowTransition
		writes      []LedgerWrite
	)

	err := s.executeTx(ctx, "quarantine logical groups", func(tx dbx.Querier) error {
		var txErr error

		result, transitions, writes, txErr = s.quarantineStaleTx(ctx, tx, at, cutoff, limit)
		if txErr != nil {
			return fmt.Errorf("quarantine logical groups: transaction body: %w", txErr)
		}

		return nil
	})
	if err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: execute transaction: %w", err)
	}

	return result, transitions, writes, nil
}

func (s *TransitionStore) quarantineStaleTx(
	ctx context.Context,
	tx dbx.Querier,
	at time.Time,
	cutoff time.Time,
	limit int,
) (QuarantineResult, []rowTransition, []LedgerWrite, error) {
	candidates, err := loadStaleSendingCandidates(ctx, tx, cutoff, limit)
	if err != nil {
		return QuarantineResult{}, nil, nil, fmt.Errorf("quarantine logical groups: load candidates: %w", err)
	}

	if len(candidates) == 0 {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyApplied, nil)}, nil, nil, nil
	}

	resolved, err := s.resolveStaleSendingGroups(ctx, tx, candidates, at)
	if err != nil {
		return QuarantineResult{}, nil, nil, fmt.Errorf("quarantine logical groups: resolve candidates: %w", err)
	}

	result, transitions, writes, err := buildQuarantineChanges(resolved, transitionRowIDSet(candidates), at, cutoff)
	if err != nil {
		return QuarantineResult{}, transitions, writes, fmt.Errorf("quarantine logical groups: build changes: %w", err)
	}

	sortTransitionsByID(transitions)

	touched, err := applyRowTransitions(ctx, tx, "quarantine logical groups", transitions)
	if err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: apply rows: %w", err)
	}

	if err := RecordDeliveryLedgerWrites(ctx, tx, LedgerStatusQuarantined, writes); err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: record ledger: %w", err)
	}

	result.ApplyResult = newApplyResult(ApplyApplied, touched)
	result.QuarantinedDeliveries = countQuarantineTransitions(transitions)

	return result, transitions, writes, nil
}

func transitionRowIDSet(rows []transitionRow) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(rows))
	for i := range rows {
		ids[rows[i].ID] = struct{}{}
	}

	return ids
}

func buildQuarantineChanges(
	resolved resolvedLogicalGroups,
	candidateIDs map[int64]struct{},
	at time.Time,
	cutoff time.Time,
) (QuarantineResult, []rowTransition, []LedgerWrite, error) {
	var (
		result      QuarantineResult
		transitions []rowTransition
		writes      []LedgerWrite
	)

	for i := range resolved.resolutions {
		change, err := quarantineResolution(
			resolved.resolutions[i], resolved, candidateIDs, at, cutoff,
		)
		if err != nil {
			return result, transitions, writes, fmt.Errorf("quarantine logical groups: resolution %d: %w", i, err)
		}

		transitions = append(transitions, change.transitions...)

		if change.write != nil {
			writes = append(writes, *change.write)
		}

		if change.blocked != nil {
			result.Blocked = append(result.Blocked, *change.blocked)
		}
	}

	return result, transitions, writes, nil
}

type quarantineResolutionChange struct {
	transitions []rowTransition
	write       *LedgerWrite
	blocked     *BlockedLogicalGroup
}

func quarantineResolution(
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
	candidateIDs map[int64]struct{},
	at time.Time,
	cutoff time.Time,
) (quarantineResolutionChange, error) {
	switch resolution.Kind() {
	case preparation.LogicalInFlight:
		change, err := quarantineInFlightResolution(resolution, resolved.rowsByID, candidateIDs, at, cutoff)
		if err != nil {
			return quarantineResolutionChange{}, fmt.Errorf("quarantine logical groups: in-flight resolution: %w", err)
		}

		return change, nil
	case preparation.LogicalFulfilled:
		transitions, err := quarantineFulfilledResolution(resolution, resolved)
		if err != nil {
			return quarantineResolutionChange{}, fmt.Errorf("quarantine logical groups: fulfilled resolution: %w", err)
		}

		return quarantineResolutionChange{transitions: transitions}, nil
	case preparation.LogicalInvariantBreach:
		blocked := &BlockedLogicalGroup{KeyHash: resolution.Key().Hash(), Reason: resolution.InvariantReason()}

		return quarantineResolutionChange{blocked: blocked}, nil
	case preparation.LogicalActive, preparation.LogicalUnresolved,
		preparation.LogicalOwnerPendingElsewhere, preparation.LogicalFailed:
		blocked := &BlockedLogicalGroup{KeyHash: resolution.Key().Hash(), Reason: preparation.InvariantNoResolvableOwner}

		return quarantineResolutionChange{blocked: blocked}, nil
	}

	return quarantineResolutionChange{}, fmt.Errorf("quarantine logical groups: unsupported resolution kind %d", resolution.Kind())
}

func quarantineInFlightResolution(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	candidateIDs map[int64]struct{},
	at time.Time,
	cutoff time.Time,
) (quarantineResolutionChange, error) {
	owner := resolution.Owner()
	if _, selected := candidateIDs[owner.DeliveryID]; !selected || owner.LockedAt.IsZero() || !owner.LockedAt.Before(cutoff) {
		return quarantineResolutionChange{}, nil
	}

	transitions := make([]rowTransition, 0, len(resolution.Members()))
	members := resolution.Members()

	for i := range members {
		member := members[i]

		row, err := transitionRowFromSnapshot(member, rowsByID)
		if err != nil {
			return quarantineResolutionChange{}, fmt.Errorf("quarantine logical groups: member row: %w", err)
		}

		transitions = append(transitions, quarantineTransition(row))
	}

	write := &LedgerWrite{Key: resolution.Key(), ObservedAt: at, SourceDeliveryID: owner.DeliveryID}

	return quarantineResolutionChange{transitions: transitions, write: write}, nil
}

func quarantineFulfilledResolution(
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
) ([]rowTransition, error) {
	ledger, ok := resolved.ledgerByKey[resolution.Key()]
	if !ok || ledger.Status != LedgerStatusSent || ledger.SentAt == nil {
		return nil, &AtomicityBreachError{Operation: "quarantine logical groups", Detail: "fulfilled group has no SENT ledger"}
	}

	transitions, err := fulfilledTransitions(resolution, resolved.rowsByID, *ledger.SentAt)
	if err != nil {
		return nil, fmt.Errorf("quarantine logical groups: fulfilled transitions: %w", err)
	}

	return transitions, nil
}

func loadStaleSendingCandidates(ctx context.Context, db dbx.Querier, cutoff time.Time, limit int) ([]transitionRow, error) {
	var candidates []transitionRow

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&candidates,
		"load stale sending transition rows",
		mustSQL("transition_stale_sending.sql"),
		lifecycle.StatusSending,
		cutoff,
		limit,
	); err != nil {
		return nil, fmt.Errorf("load stale sending transition rows: %w", err)
	}

	return candidates, nil
}

func (s *TransitionStore) resolveStaleSendingGroups(
	ctx context.Context,
	db dbx.Querier,
	candidates []transitionRow,
	at time.Time,
) (resolvedLogicalGroups, error) {
	requested, requestedKeys, err := staleRequestedLogicalGroups(candidates)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: build requests: %w", err)
	}

	rows, err := s.loadLogicalGroupRows(ctx, db, requested)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: load rows: %w", err)
	}

	rowsByID, snapshots, err := staleResolutionSnapshots(rows, requestedKeys, s.config.LogicalGroupLimit)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: build snapshots: %w", err)
	}

	ledgerByKey, evidence, err := loadTransitionLedger(ctx, db, requestedKeys)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: load ledger: %w", err)
	}

	resolver, err := preparation.NewResolver(preparation.ResolverConfig{
		LogicalGroupScanLimit: s.config.LogicalGroupLimit,
		RetryBackoff:          s.config.RetryBackoff,
		LockTimeout:           s.config.LockTimeout,
		RequireTerminalLedger: true,
	})
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: create resolver: %w", err)
	}

	return resolvedLogicalGroups{
		resolutions: resolver.ResolveGroups(snapshots, evidence, requestedKeys, at),
		rowsByID:    rowsByID, ledgerByKey: ledgerByKey,
	}, nil
}

func staleRequestedLogicalGroups(
	candidates []transitionRow,
) ([]requestedLogicalGroup, []ytcontentid.LogicalKey, error) {
	requested := make([]requestedLogicalGroup, 0, len(candidates))
	keys := make([]ytcontentid.LogicalKey, 0, len(candidates))

	for i := range candidates {
		key, err := candidates[i].logicalKey()
		if err != nil {
			return nil, nil, fmt.Errorf("resolve stale sending groups: %w", err)
		}

		outbox := candidates[i].domainOutbox()

		requested = append(requested, requestedLogicalGroup{
			key: key, delivery: candidates[i].domainDelivery(), outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
		keys = append(keys, key)
	}

	return requested, uniqueLogicalKeys(keys), nil
}

func staleResolutionSnapshots(
	rows []transitionRow,
	requestedKeys []ytcontentid.LogicalKey,
	logicalGroupLimit int,
) (map[int64]transitionRow, []preparation.DeliverySnapshot, error) {
	requested := make(map[ytcontentid.LogicalKey]struct{}, len(requestedKeys))
	for i := range requestedKeys {
		requested[requestedKeys[i]] = struct{}{}
	}

	rowsByID := make(map[int64]transitionRow, len(rows))
	snapshots := make([]preparation.DeliverySnapshot, 0, len(rows))
	counts := make(map[ytcontentid.LogicalKey]int, len(requestedKeys))

	for i := range rows {
		key, keyErr := rows[i].logicalKey()
		if keyErr != nil {
			continue
		}

		if _, wanted := requested[key]; !wanted {
			continue
		}

		counts[key]++
		if counts[key] > logicalGroupLimit {
			return nil, nil, fmt.Errorf("resolve stale sending groups: logical group %s exceeds limit %d", key.Hash(), logicalGroupLimit)
		}

		snapshot, err := rows[i].snapshot(false)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve stale sending groups: snapshot: %w", err)
		}

		rowsByID[rows[i].ID] = rows[i]

		snapshots = append(snapshots, snapshot)
	}

	return rowsByID, snapshots, nil
}

func quarantineTransition(row transitionRow) rowTransition {
	after := row

	after.Status = lifecycle.StatusQuarantined
	after.RowVersion++

	after.LockedAt = nil
	after.SentAt = nil
	after.Error = staleSendingQuarantineReason

	return rowTransition{before: row, after: after}
}

func countQuarantineTransitions(transitions []rowTransition) int {
	count := 0

	for i := range transitions {
		if transitions[i].after.Status == lifecycle.StatusQuarantined {
			count++
		}
	}

	return count
}

func validateQuarantineLedger(ctx context.Context, db dbx.Querier, writes []LedgerWrite) error {
	keys := make([]ytcontentid.LogicalKey, 0, len(writes))
	for i := range writes {
		keys = append(keys, writes[i].Key)
	}

	ledger, _, err := loadTransitionLedger(ctx, db, keys)
	if err != nil {
		return fmt.Errorf("quarantine logical groups: read-back ledger: %w", err)
	}

	for i := range keys {
		record, ok := ledger[keys[i]]
		if !ok || (record.Status != LedgerStatusQuarantined && record.Status != LedgerStatusSent) {
			return &AtomicityBreachError{
				Operation: "quarantine logical groups", Detail: fmt.Sprintf("terminal ledger is absent for key %s", keys[i].Hash()),
			}
		}
	}

	return nil
}

func uniqueLogicalKeys(keys []ytcontentid.LogicalKey) []ytcontentid.LogicalKey {
	seen := make(map[ytcontentid.LogicalKey]struct{}, len(keys))
	result := make([]ytcontentid.LogicalKey, 0, len(keys))

	for i := range keys {
		if _, ok := seen[keys[i]]; ok {
			continue
		}

		seen[keys[i]] = struct{}{}
		result = append(result, keys[i])
	}

	return result
}

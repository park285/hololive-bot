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
)

const staleSendingQuarantineReason = "stale sending; external send outcome unknown"

type QuarantineResult struct {
	ApplyResult

	QuarantinedDeliveries int
	Blocked               []BlockedLogicalGroup
}

func (s *TransitionStore) QuarantineStaleLogicalGroups(ctx context.Context, limit int) (QuarantineResult, error) {
	if limit <= 0 {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyConflict, nil)}, errors.New("quarantine stale logical groups: limit must be positive")
	}

	var priorAdjudication CommitAdjudication

	for attempt := 0; attempt <= transitionOperationRetryLimit; attempt++ {
		result, transitions, writes, applyErr := s.quarantineStaleOnce(ctx, limit)
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
	limit int,
) (QuarantineResult, []rowTransition, []LedgerWrite, error) {
	var (
		result      QuarantineResult
		transitions []rowTransition
		writes      []LedgerWrite
	)

	err := s.executeTx(ctx, "quarantine logical groups", func(tx dbx.Querier) error {
		var txErr error

		result, transitions, writes, txErr = s.quarantineStaleTx(ctx, tx, limit)
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

// quarantineStaleTx는 lease가 끝난 SENDING 그룹을 격리한다. 만료 판정은 후보 SQL의 DB 시계(checked_at)로, ledger
// 관측 시각과 결과 불명 시도의 종료 시각은 격리 전이 문장의 DB 시계로 기록한다.
func (s *TransitionStore) quarantineStaleTx(
	ctx context.Context,
	tx dbx.Querier,
	limit int,
) (QuarantineResult, []rowTransition, []LedgerWrite, error) {
	candidates, checkedAt, err := loadStaleSendingCandidates(ctx, tx, s.config.LockTimeout, limit)
	if err != nil {
		return QuarantineResult{}, nil, nil, fmt.Errorf("quarantine logical groups: load candidates: %w", err)
	}

	if len(candidates) == 0 {
		return QuarantineResult{ApplyResult: newApplyResult(ApplyApplied, nil)}, nil, nil, nil
	}

	resolved, err := s.resolveStaleSendingGroups(ctx, tx, candidates)
	if err != nil {
		return QuarantineResult{}, nil, nil, fmt.Errorf("quarantine logical groups: resolve candidates: %w", err)
	}

	cutoff := checkedAt.Add(-s.config.LockTimeout)

	result, transitions, writes, attempts, err := buildQuarantineChanges(resolved, transitionRowIDSet(candidates), cutoff)
	if err != nil {
		return QuarantineResult{}, transitions, writes, fmt.Errorf("quarantine logical groups: build changes: %w", err)
	}

	sortTransitionsByID(transitions)

	touched, quarantinedAt, err := applyRowTransitions(ctx, tx, "quarantine logical groups", transitions)
	if err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: apply rows: %w", err)
	}

	for i := range writes {
		writes[i].ObservedAt = quarantinedAt
	}

	if err := RecordDeliveryLedgerWrites(ctx, tx, LedgerStatusQuarantined, writes); err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: record ledger: %w", err)
	}

	// 결과 불명으로 격리한 owner 시도는 공백으로 두지 않고 같은 트랜잭션에서 outcome_unknown으로 기록한다.
	if err := recordAttemptTelemetry(ctx, tx, deliveryModeStaleSweep, attempts, quarantinedAt); err != nil {
		return result, transitions, writes, fmt.Errorf("quarantine logical groups: record telemetry: %w", err)
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
	cutoff time.Time,
) (QuarantineResult, []rowTransition, []LedgerWrite, []deliveryAttempt, error) {
	var (
		result      QuarantineResult
		transitions []rowTransition
		writes      []LedgerWrite
		attempts    []deliveryAttempt
	)

	for i := range resolved.resolutions {
		change, err := quarantineResolution(
			resolved.resolutions[i], resolved, candidateIDs, cutoff,
		)
		if err != nil {
			return result, transitions, writes, attempts, fmt.Errorf("quarantine logical groups: resolution %d: %w", i, err)
		}

		transitions = append(transitions, change.transitions...)

		if change.write != nil {
			writes = append(writes, *change.write)
		}

		if change.attempt != nil {
			attempts = append(attempts, *change.attempt)
		}

		if change.blocked != nil {
			result.Blocked = append(result.Blocked, *change.blocked)
		}
	}

	return result, transitions, writes, attempts, nil
}

type quarantineResolutionChange struct {
	transitions []rowTransition
	write       *LedgerWrite
	// attempt는 격리한 in-flight owner의 결과 불명 시도다. fulfilled 전파는 시도가 아니라 비워 둔다.
	attempt *deliveryAttempt
	blocked *BlockedLogicalGroup
}

func quarantineResolution(
	resolution preparation.Resolution,
	resolved resolvedLogicalGroups,
	candidateIDs map[int64]struct{},
	cutoff time.Time,
) (quarantineResolutionChange, error) {
	switch resolution.Kind() {
	case preparation.LogicalInFlight:
		change, err := quarantineInFlightResolution(resolution, resolved.rowsByID, candidateIDs, cutoff)
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

// quarantineInFlightResolution은 후보로 잠근 owner의 lease가 cutoff(후보 SQL의 DB 시계 - LockTimeout) 전에 끝났을 때만
// 그룹을 격리하고, ledger 관측 시각은 전이 뒤에 채운다.
func quarantineInFlightResolution(
	resolution preparation.Resolution,
	rowsByID map[int64]transitionRow,
	candidateIDs map[int64]struct{},
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

	ownerRow, err := transitionRowFromSnapshot(owner, rowsByID)
	if err != nil {
		return quarantineResolutionChange{}, fmt.Errorf("quarantine logical groups: owner row: %w", err)
	}

	write := &LedgerWrite{Key: resolution.Key(), SourceDeliveryID: owner.DeliveryID}
	attempt := &deliveryAttempt{
		group:  startedLogicalGroup{key: resolution.Key(), ownerBefore: ownerRow, ownerAfter: ownerRow},
		result: attemptResultOutcomeUnknown,
		reason: staleSendingOutcomeUnknownReason,
	}

	return quarantineResolutionChange{transitions: transitions, write: write, attempt: attempt}, nil
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

// loadStaleSendingCandidates는 lease가 끝난 SENDING 행을 잠그고, 판정에 쓴 DB 시계를 함께 돌려준다.
func loadStaleSendingCandidates(
	ctx context.Context,
	db dbx.Querier,
	lease time.Duration,
	limit int,
) ([]transitionRow, time.Time, error) {
	var checked []checkedTransitionRow

	if err := dbx.SelectSQL(
		ctx,
		db,
		&checked,
		"load stale sending transition rows",
		mustSQL("transition_stale_sending.sql"),
		lifecycle.StatusSending,
		lease.Milliseconds(),
		limit,
	); err != nil {
		return nil, time.Time{}, fmt.Errorf("load stale sending transition rows: %w", err)
	}

	candidates, checkedAt := splitCheckedTransitionRows(checked)

	return candidates, checkedAt, nil
}

func (s *TransitionStore) resolveStaleSendingGroups(
	ctx context.Context,
	db dbx.Querier,
	candidates []transitionRow,
) (resolvedLogicalGroups, error) {
	requested, requestedKeys, err := staleRequestedLogicalGroups(candidates)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: build requests: %w", err)
	}

	rows, err := s.loadLogicalGroupRows(ctx, db, requested)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: load rows: %w", err)
	}

	if !s.logicalGroupWindowComplete(len(requested), len(rows)) {
		return resolvedLogicalGroups{}, errors.New("resolve stale sending groups: logical group window is incomplete")
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
		LockTimeout:           s.config.LockTimeout,
		RequireTerminalLedger: true,
	})
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve stale sending groups: create resolver: %w", err)
	}

	return resolvedLogicalGroups{
		resolutions: resolver.ResolveGroups(snapshots, evidence, requestedKeys),
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

		rowsByID[rows[i].ID] = rows[i]

		snapshots = append(snapshots, rows[i].snapshot(false))
	}

	return rowsByID, snapshots, nil
}

func quarantineTransition(row transitionRow) rowTransition {
	after := row

	after.Status = lifecycle.StatusQuarantined
	after.RowVersion++

	after.LockedAt = nil
	after.LockedBy = nil
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

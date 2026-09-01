package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

const transitionOperationRetryLimit = 1

type TransitionStore struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
	config TransitionConfig
	ready  atomic.Bool

	// afterCommit is a package-private fault-injection hook. It runs only after
	// PostgreSQL accepted COMMIT and lets tests exercise response-loss read-back.
	afterCommit func(operation string) error
}

func NewTransitionStore(db any, logger *slog.Logger, config TransitionConfig) (*TransitionStore, error) {
	deliveryDB := AsDeliveryDB(db)
	if deliveryDB == nil {
		return nil, errors.New("new transition store: db is nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	if config.MaxRetries <= 0 {
		return nil, errors.New("new transition store: max retries must be positive")
	}

	if config.RetryBackoff <= 0 {
		return nil, errors.New("new transition store: retry backoff must be positive")
	}

	if config.LockTimeout <= 0 {
		return nil, errors.New("new transition store: lock timeout must be positive")
	}

	if config.ClaimFreshnessWindow <= 0 {
		return nil, errors.New("new transition store: claim freshness window must be positive")
	}

	if config.LogicalGroupLimit <= 0 {
		return nil, errors.New("new transition store: logical group limit must be positive")
	}

	return &TransitionStore{db: deliveryDB, logger: logger, config: config}, nil
}

func (s *TransitionStore) ensureReady(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("transition store: db is nil")
	}

	if s.ready.Load() {
		return nil
	}

	var ready bool

	if err := s.db.QueryRow(ctx, mustSQL("transition_ready.sql"), LedgerSchemaVersion).Scan(&ready); err != nil {
		return fmt.Errorf("transition store: load ledger completion: %w", err)
	}

	if !ready {
		return errors.New("transition store: ledger backfill is not complete")
	}

	s.ready.Store(true)

	return nil
}

func (s *TransitionStore) ClaimPending(ctx context.Context, batchSize int) ([]domain.YouTubeNotificationDelivery, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, fmt.Errorf("claim pending: %w", err)
	}

	if batchSize <= 0 {
		return nil, errors.New("claim pending: batch size must be positive")
	}

	claimedAt, err := lifecycle.CanonicalTime(time.Now())
	if err != nil {
		return nil, fmt.Errorf("claim pending: claim at: %w", err)
	}

	lockCutoff := claimedAt.Add(-s.config.LockTimeout)
	freshCutoff := claimedAt.Add(-s.config.ClaimFreshnessWindow)

	rows, err := s.db.Query(
		ctx,
		mustSQL("transition_claim_pending.sql"),
		lifecycle.StatusPending,
		lockCutoff,
		claimedAt,
		freshCutoff,
		batchSize,
		claimedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("claim pending: update rows: %w", err)
	}
	defer rows.Close()

	claimed, err := pgx.CollectRows(rows, scanTransitionDelivery)
	if err != nil {
		return nil, fmt.Errorf("claim pending: collect rows: %w", err)
	}

	return claimed, nil
}

func scanTransitionDelivery(row pgx.CollectableRow) (domain.YouTubeNotificationDelivery, error) {
	var delivery domain.YouTubeNotificationDelivery

	if err := row.Scan(
		&delivery.ID,
		&delivery.OutboxID,
		&delivery.RoomID,
		&delivery.Status,
		&delivery.AttemptCount,
		&delivery.NextAttemptAt,
		&delivery.CreatedAt,
		&delivery.LockedAt,
		&delivery.SentAt,
		&delivery.Error,
		&delivery.RowVersion,
	); err != nil {
		return domain.YouTubeNotificationDelivery{}, fmt.Errorf("scan transition delivery: %w", err)
	}

	return delivery, nil
}

type requestedLogicalGroup struct {
	key       ytcontentid.LogicalKey
	delivery  domain.YouTubeNotificationDelivery
	outbox    domain.YouTubeNotificationOutbox
	candidate []string
}

type resolvedLogicalGroups struct {
	resolutions []preparation.Resolution
	rowsByID    map[int64]transitionRow
	ledgerByKey map[ytcontentid.LogicalKey]DeliveryLedgerRecord
}

func (s *TransitionStore) resolveClaimedGroups(
	ctx context.Context,
	db dbx.Querier,
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
	at time.Time,
) (resolvedLogicalGroups, error) {
	requested, err := buildRequestedLogicalGroups(claimed, outboxByID)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: requested keys: %w", err)
	}

	rows, err := s.loadLogicalGroupRows(ctx, db, requested)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: load rows: %w", err)
	}

	currentIDs := deliveryIDSet(claimed)
	requestedKeys, requestedSet := requestedLogicalKeys(requested)

	rowsByID, snapshots, err := s.claimedResolutionSnapshots(rows, currentIDs, requestedSet)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: build snapshots: %w", err)
	}

	if validationErr := validateClaimedRowsPresent(currentIDs, rowsByID); validationErr != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: validate current rows: %w", validationErr)
	}

	ledgerByKey, evidence, err := loadTransitionLedger(ctx, db, requestedKeys)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: load ledger: %w", err)
	}

	resolver, err := preparation.NewResolver(preparation.ResolverConfig{
		LogicalGroupScanLimit: s.config.LogicalGroupLimit,
		RetryBackoff:          s.config.RetryBackoff,
		LockTimeout:           s.config.LockTimeout,
		RequireTerminalLedger: true,
	})
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: resolver: %w", err)
	}

	return resolvedLogicalGroups{
		resolutions: resolver.ResolveGroups(snapshots, evidence, requestedKeys, at),
		rowsByID:    rowsByID,
		ledgerByKey: ledgerByKey,
	}, nil
}

func requestedLogicalKeys(
	requested []requestedLogicalGroup,
) ([]ytcontentid.LogicalKey, map[ytcontentid.LogicalKey]struct{}) {
	keys := make([]ytcontentid.LogicalKey, 0, len(requested))
	keySet := make(map[ytcontentid.LogicalKey]struct{}, len(requested))

	for i := range requested {
		if _, ok := keySet[requested[i].key]; ok {
			continue
		}

		keySet[requested[i].key] = struct{}{}
		keys = append(keys, requested[i].key)
	}

	return keys, keySet
}

func (s *TransitionStore) claimedResolutionSnapshots(
	rows []transitionRow,
	currentIDs map[int64]struct{},
	requestedSet map[ytcontentid.LogicalKey]struct{},
) (map[int64]transitionRow, []preparation.DeliverySnapshot, error) {
	rowsByID := make(map[int64]transitionRow, len(rows))
	snapshots := make([]preparation.DeliverySnapshot, 0, len(rows))
	countsByKey := make(map[ytcontentid.LogicalKey]int, len(requestedSet))

	for i := range rows {
		key, snapshot, include, err := claimedResolutionSnapshot(rows[i], currentIDs, requestedSet)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve claimed groups: row snapshot: %w", err)
		}

		if !include {
			continue
		}

		countsByKey[key]++
		if countsByKey[key] > s.config.LogicalGroupLimit {
			return nil, nil, fmt.Errorf("resolve claimed groups: logical group %s exceeds limit %d", key.Hash(), s.config.LogicalGroupLimit)
		}

		rowsByID[rows[i].ID] = rows[i]

		snapshots = append(snapshots, snapshot)
	}

	return rowsByID, snapshots, nil
}

func claimedResolutionSnapshot(
	row transitionRow,
	currentIDs map[int64]struct{},
	requestedSet map[ytcontentid.LogicalKey]struct{},
) (ytcontentid.LogicalKey, preparation.DeliverySnapshot, bool, error) {
	key, err := row.logicalKey()
	_, current := currentIDs[row.ID]

	if err != nil {
		if current {
			return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, fmt.Errorf("resolve claimed groups: current row identity: %w", err)
		}

		return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, nil
	}

	if _, wanted := requestedSet[key]; !wanted {
		return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, nil
	}

	snapshot, err := row.snapshot(current)
	if err != nil {
		return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, fmt.Errorf("resolve claimed groups: snapshot: %w", err)
	}

	return key, snapshot, true, nil
}

func validateClaimedRowsPresent(
	currentIDs map[int64]struct{},
	rowsByID map[int64]transitionRow,
) error {
	for id := range currentIDs {
		if _, ok := rowsByID[id]; !ok {
			return &transitionMissingError{
				operation: "resolve claimed groups", detail: fmt.Sprintf("claimed delivery %d is absent", id),
			}
		}
	}

	return nil
}

func buildRequestedLogicalGroups(
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) ([]requestedLogicalGroup, error) {
	if len(claimed) == 0 {
		return nil, nil
	}

	requested := make([]requestedLogicalGroup, 0, len(claimed))
	for i := range claimed {
		outbox, ok := outboxByID[claimed[i].OutboxID]
		if !ok {
			return nil, fmt.Errorf("delivery %d outbox %d is absent", claimed[i].ID, claimed[i].OutboxID)
		}

		key, err := ytcontentid.ResolveDeliveryKey(outbox.Kind, outbox.ContentID, outbox.Payload, claimed[i].RoomID)
		if err != nil {
			return nil, fmt.Errorf("delivery %d logical key: %w", claimed[i].ID, err)
		}

		requested = append(requested, requestedLogicalGroup{
			key: key, delivery: claimed[i], outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
	}

	return requested, nil
}

func logicalIdentityCandidates(key ytcontentid.LogicalKey, outbox domain.YouTubeNotificationOutbox) []string {
	values := []string{strings.TrimSpace(outbox.ContentID), key.LogicalID}
	if raw, ok := strings.CutPrefix(key.LogicalID, "short:"); ok {
		values = append(values, raw)
	}

	if raw, ok := strings.CutPrefix(key.LogicalID, "community:"); ok {
		values = append(values, raw)
	}

	return UniqueStrings(values)
}

func (s *TransitionStore) loadLogicalGroupRows(
	ctx context.Context,
	db dbx.Querier,
	requested []requestedLogicalGroup,
) ([]transitionRow, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(requested))
	roomIDs := make([]string, 0, len(requested))
	kinds := make([]string, 0, len(requested))
	candidates := make([]string, 0, len(requested)*3)

	for i := range requested {
		ids = append(ids, requested[i].delivery.ID)
		roomIDs = append(roomIDs, requested[i].key.RoomID)
		kinds = append(kinds, string(requested[i].key.Kind))
		candidates = append(candidates, requested[i].candidate...)
	}

	roomIDs = UniqueStrings(roomIDs)
	kinds = UniqueStrings(kinds)
	candidates = UniqueStrings(candidates)

	limit := len(requested)*(s.config.LogicalGroupLimit+1) + len(ids)

	var rows []transitionRow

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&rows,
		"load transition logical group rows",
		mustSQL("transition_group_rows.sql"),
		ids,
		roomIDs,
		kinds,
		candidates,
		limit,
	); err != nil {
		return nil, fmt.Errorf("load transition logical group rows: %w", err)
	}

	return rows, nil
}

func loadTransitionLedger(
	ctx context.Context,
	db dbx.Querier,
	keys []ytcontentid.LogicalKey,
) (map[ytcontentid.LogicalKey]DeliveryLedgerRecord, []preparation.LedgerEvidence, error) {
	ledgerByKey := make(map[ytcontentid.LogicalKey]DeliveryLedgerRecord, len(keys))
	if len(keys) == 0 {
		return ledgerByKey, nil, nil
	}

	kinds := make([]string, 0, len(keys))
	logicalIDs := make([]string, 0, len(keys))
	roomIDs := make([]string, 0, len(keys))

	for i := range keys {
		kinds = append(kinds, string(keys[i].Kind))
		logicalIDs = append(logicalIDs, keys[i].LogicalID)
		roomIDs = append(roomIDs, keys[i].RoomID)
	}

	var records []DeliveryLedgerRecord

	if err := deliverysql.SelectDeliverySQL(
		ctx,
		db,
		&records,
		"load transition ledger rows",
		mustSQL("transition_ledger_rows.sql"),
		kinds,
		logicalIDs,
		roomIDs,
	); err != nil {
		return nil, nil, fmt.Errorf("load transition ledger rows: %w", err)
	}

	evidence := make([]preparation.LedgerEvidence, 0, len(records))
	for i := range records {
		key := ytcontentid.LogicalKey{
			Kind: records[i].Kind, LogicalID: records[i].LogicalID, RoomID: records[i].RoomID,
		}

		ledgerByKey[key] = records[i]

		status := lifecycle.LedgerStatus(records[i].Status)

		evidence = append(evidence, preparation.LedgerEvidence{
			Key: key, Status: status, RecordedAt: records[i].FirstRecordedAt,
		})
	}

	return ledgerByKey, evidence, nil
}

type rowTransition struct {
	before transitionRow
	after  transitionRow
}

type transitionVectors struct {
	ids              []int64
	expectedStatuses []string
	expectedVersions []int64
	expectedAttempts []int32
	expectedLocks    []*time.Time
	nextStatuses     []string
	nextVersions     []int64
	nextAttempts     []int32
	nextAttemptAts   []time.Time
	nextLocks        []*time.Time
	nextSentAts      []*time.Time
	nextErrors       []string
}

func applyRowTransitions(ctx context.Context, db dbx.Querier, operation string, transitions []rowTransition) ([]int64, error) {
	if len(transitions) == 0 {
		return nil, nil
	}

	vectors, err := buildTransitionVectors(operation, transitions)
	if err != nil {
		return nil, fmt.Errorf("%s: build transition vectors: %w", operation, err)
	}

	rows, err := db.Query(
		ctx,
		mustSQL("transition_apply_rows.sql"),
		vectors.ids,
		vectors.expectedStatuses,
		vectors.expectedVersions,
		vectors.expectedAttempts,
		vectors.expectedLocks,
		vectors.nextStatuses,
		vectors.nextVersions,
		vectors.nextAttempts,
		vectors.nextAttemptAts,
		vectors.nextLocks,
		vectors.nextSentAts,
		vectors.nextErrors,
	)
	if err != nil {
		return nil, fmt.Errorf("%s: update rows: %w", operation, err)
	}
	defer rows.Close()

	updatedIDs, outboxIDs, err := collectAppliedTransitions(rows, operation, len(transitions))
	if err != nil {
		return nil, fmt.Errorf("%s: collect applied transitions: %w", operation, err)
	}

	if !equalSortedIDs(updatedIDs, vectors.ids) {
		return nil, &transitionConflictError{
			operation: operation,
			detail:    fmt.Sprintf("updated ids %v do not match expected ids %v", updatedIDs, vectors.ids),
		}
	}

	return uniqueSortedInt64s(outboxIDs), nil
}

func buildTransitionVectors(operation string, transitions []rowTransition) (transitionVectors, error) {
	vectors := newTransitionVectors(len(transitions))

	for i := range transitions {
		expectedAttempt, err := checkedTransitionAttempt(operation, transitions[i].before)
		if err != nil {
			return transitionVectors{}, fmt.Errorf("%s: validate expected attempt: %w", operation, err)
		}

		nextAttempt, err := checkedTransitionAttempt(operation, transitions[i].after)
		if err != nil {
			return transitionVectors{}, fmt.Errorf("%s: validate next attempt: %w", operation, err)
		}

		vectors.appendTransition(transitions[i], expectedAttempt, nextAttempt)
	}

	return vectors, nil
}

func newTransitionVectors(capacity int) transitionVectors {
	return transitionVectors{
		ids: make([]int64, 0, capacity), expectedStatuses: make([]string, 0, capacity),
		expectedVersions: make([]int64, 0, capacity), expectedAttempts: make([]int32, 0, capacity),
		expectedLocks: make([]*time.Time, 0, capacity), nextStatuses: make([]string, 0, capacity),
		nextVersions: make([]int64, 0, capacity), nextAttempts: make([]int32, 0, capacity),
		nextAttemptAts: make([]time.Time, 0, capacity), nextLocks: make([]*time.Time, 0, capacity),
		nextSentAts: make([]*time.Time, 0, capacity), nextErrors: make([]string, 0, capacity),
	}
}

func (v *transitionVectors) appendTransition(transition rowTransition, expectedAttempt, nextAttempt int32) {
	v.ids = append(v.ids, transition.before.ID)
	v.expectedStatuses = append(v.expectedStatuses, string(transition.before.Status))
	v.expectedVersions = append(v.expectedVersions, transition.before.RowVersion)
	v.expectedAttempts = append(v.expectedAttempts, expectedAttempt)
	v.expectedLocks = append(v.expectedLocks, cloneTimePtr(transition.before.LockedAt))
	v.nextStatuses = append(v.nextStatuses, string(transition.after.Status))
	v.nextVersions = append(v.nextVersions, transition.after.RowVersion)
	v.nextAttempts = append(v.nextAttempts, nextAttempt)
	v.nextAttemptAts = append(v.nextAttemptAts, transition.after.NextAttemptAt.UTC())
	v.nextLocks = append(v.nextLocks, cloneTimePtr(transition.after.LockedAt))
	v.nextSentAts = append(v.nextSentAts, cloneTimePtr(transition.after.SentAt))
	v.nextErrors = append(v.nextErrors, deliverysql.TruncateString(transition.after.Error, 500))
}

func checkedTransitionAttempt(operation string, row transitionRow) (int32, error) {
	if row.AttemptCount < math.MinInt32 || row.AttemptCount > math.MaxInt32 {
		return 0, fmt.Errorf("%s: delivery %d attempt count %d exceeds database integer range", operation, row.ID, row.AttemptCount)
	}

	return int32(row.AttemptCount), nil
}

func collectAppliedTransitions(rows pgx.Rows, operation string, capacity int) ([]int64, []int64, error) {
	updatedIDs := make([]int64, 0, capacity)
	outboxIDs := make([]int64, 0, capacity)

	for rows.Next() {
		var deliveryID, outboxID int64

		if err := rows.Scan(&deliveryID, &outboxID); err != nil {
			return nil, nil, fmt.Errorf("%s: scan updated row: %w", operation, err)
		}

		updatedIDs = append(updatedIDs, deliveryID)
		outboxIDs = append(outboxIDs, outboxID)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("%s: iterate updated rows: %w", operation, err)
	}

	return updatedIDs, outboxIDs, nil
}

func equalSortedIDs(actual, expected []int64) bool {
	slices.Sort(actual)

	expected = slices.Clone(expected)
	slices.Sort(expected)

	return slices.Equal(actual, expected)
}

func (s *TransitionStore) executeTx(
	ctx context.Context,
	operation string,
	fn func(tx dbx.Querier) error,
) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%s: begin transaction: %w", operation, err)
	}

	if err := fn(tx); err != nil {
		rollbackErr := tx.Rollback(ctx)
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%s: command failed and rollback failed: %w", operation, errors.Join(err, rollbackErr))
		}

		return fmt.Errorf("%s: transaction command: %w", operation, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return &commitResponseError{operation: operation, err: fmt.Errorf("commit transaction: %w", err)}
	}

	if s.afterCommit != nil {
		if err := s.afterCommit(operation); err != nil {
			return &commitResponseError{operation: operation, err: err}
		}
	}

	return nil
}

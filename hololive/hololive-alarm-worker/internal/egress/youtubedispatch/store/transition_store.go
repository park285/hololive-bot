package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/preparation"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

const transitionOperationRetryLimit = 1

type TransitionStore struct {
	db     deliverysql.DeliveryDB
	logger *slog.Logger
	config TransitionConfig

	afterCommit func(operation string) error
}

func NewTransitionStore(db deliverysql.DeliveryDB, logger *slog.Logger, config TransitionConfig) (*TransitionStore, error) {
	if db == nil || reflectutil.IsNil(db) {
		return nil, errors.New("new transition store: db is nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	if strings.TrimSpace(config.Owner) == "" {
		return nil, errors.New("new transition store: owner is required")
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

	return &TransitionStore{db: db, logger: logger, config: config}, nil
}

func (s *TransitionStore) ClaimPending(ctx context.Context, batchSize int) ([]domain.YouTubeNotificationDelivery, error) {
	if batchSize <= 0 {
		return nil, errors.New("claim pending: batch size must be positive")
	}

	rows, err := pgxdb.Query(ctx, s.db,
		transitionClaimPendingSQL,
		lifecycle.StatusPending,
		s.config.LockTimeout.Milliseconds(),
		s.config.ClaimFreshnessWindow.Milliseconds(),
		batchSize,
		s.config.Owner,
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
		&delivery.LockedBy,
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
	db pgxdb.Querier,
	operation string,
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
) (resolvedLogicalGroups, error) {
	requested, err := buildRequestedLogicalGroups(claimed, outboxByID)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: requested keys: %w", err)
	}

	claimFence := func(rows []transitionRow) error {
		return checkClaimFence(operation, claimed, rows)
	}

	resolved, err := s.resolveLogicalGroups(ctx, db, requested, deliveryIDSet(claimed), claimFence)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: %w", err)
	}

	return resolved, nil
}

func (s *TransitionStore) resolveLogicalGroups(
	ctx context.Context,
	db pgxdb.Querier,
	requested []requestedLogicalGroup,
	currentIDs map[int64]struct{},
	checkLocked func(rows []transitionRow) error,
) (resolvedLogicalGroups, error) {
	rows, err := s.loadLogicalGroupRows(ctx, db, requested)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve logical groups: load rows: %w", err)
	}

	if !s.logicalGroupWindowComplete(len(requested), len(rows)) {
		return resolvedLogicalGroups{}, errors.New("resolve logical groups: logical group window is incomplete")
	}

	requestedKeys, requestedSet := requestedLogicalKeys(requested)

	rowsByID, snapshots, err := s.resolutionSnapshots(rows, currentIDs, requestedSet)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve logical groups: build snapshots: %w", err)
	}

	if checkLocked != nil {
		if lockedErr := checkLocked(rows); lockedErr != nil {
			return resolvedLogicalGroups{}, fmt.Errorf("resolve logical groups: check locked rows: %w", lockedErr)
		}
	}

	ledgerByKey, evidence, err := loadTransitionLedger(ctx, db, requestedKeys)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve logical groups: load ledger: %w", err)
	}

	resolver, err := preparation.NewResolver(preparation.ResolverConfig{
		LogicalGroupScanLimit: s.config.LogicalGroupLimit,
		LockTimeout:           s.config.LockTimeout,
	})
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve logical groups: create resolver: %w", err)
	}

	return resolvedLogicalGroups{
		resolutions: resolver.ResolveGroups(snapshots, evidence, requestedKeys),
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

func (s *TransitionStore) resolutionSnapshots(
	rows []transitionRow,
	currentIDs map[int64]struct{},
	requestedSet map[ytcontentid.LogicalKey]struct{},
) (map[int64]transitionRow, []preparation.DeliverySnapshot, error) {
	rowsByID := make(map[int64]transitionRow, len(rows))
	snapshots := make([]preparation.DeliverySnapshot, 0, len(rows))
	countsByKey := make(map[ytcontentid.LogicalKey]int, len(requestedSet))

	for i := range rows {
		key, snapshot, include, err := resolutionSnapshot(rows[i], currentIDs, requestedSet)
		if err != nil {
			return nil, nil, fmt.Errorf("build resolution snapshots: row snapshot: %w", err)
		}

		if !include {
			continue
		}

		countsByKey[key]++
		if countsByKey[key] > s.config.LogicalGroupLimit {
			return nil, nil, fmt.Errorf("build resolution snapshots: logical group %s exceeds limit %d", key.Hash(), s.config.LogicalGroupLimit)
		}

		rowsByID[rows[i].ID] = rows[i]

		snapshots = append(snapshots, snapshot)
	}

	return rowsByID, snapshots, nil
}

func resolutionSnapshot(
	row transitionRow,
	currentIDs map[int64]struct{},
	requestedSet map[ytcontentid.LogicalKey]struct{},
) (ytcontentid.LogicalKey, preparation.DeliverySnapshot, bool, error) {
	key, err := row.logicalKey()
	_, current := currentIDs[row.ID]

	if err != nil {
		if current {
			return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, fmt.Errorf("build resolution snapshot: current row identity: %w", err)
		}

		return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, nil
	}

	if _, wanted := requestedSet[key]; !wanted {
		return ytcontentid.LogicalKey{}, preparation.DeliverySnapshot{}, false, nil
	}

	return key, row.snapshot(current), true, nil
}

func checkClaimFence(operation string, claimed []domain.YouTubeNotificationDelivery, rows []transitionRow) error {
	loaded := make(map[int64]transitionRow, len(rows))
	for i := range rows {
		loaded[rows[i].ID] = rows[i]
	}

	want := make([]int64, 0, len(claimed))
	observed := make([]sendfence.Observation, 0, len(claimed))
	seen := make(map[int64]struct{}, len(claimed))

	for i := range claimed {
		if _, duplicate := seen[claimed[i].ID]; duplicate {
			continue
		}

		seen[claimed[i].ID] = struct{}{}
		want = append(want, claimed[i].ID)

		if row, ok := loaded[claimed[i].ID]; ok {
			observed = append(observed, claimObservation(claimed[i], row))
		}
	}

	held := len(observed) == len(want) && !slices.ContainsFunc(observed, func(observation sendfence.Observation) bool {
		return !observation.StatusMatches || !observation.OwnerMatches || !observation.VersionMatches
	})
	if !held {
		return fmt.Errorf("check claim fence: %w", sendfence.Check(operation, want, observed))
	}

	for i := range observed {
		if row := loaded[observed[i].ID]; row.LockedAt == nil || row.RowVersion <= 0 {
			return fmt.Errorf("%s: delivery %d was not claimed", operation, row.ID)
		}
	}

	return nil
}

func claimObservation(claim domain.YouTubeNotificationDelivery, row transitionRow) sendfence.Observation {
	return sendfence.Observation{
		ID: row.ID, Status: string(row.Status),
		StatusMatches:  row.Status == lifecycle.StatusPending,
		OwnerMatches:   sameOwner(row.LockedBy, claim.LockedBy),
		VersionMatches: row.RowVersion == claim.RowVersion,
		LeaseValid:     true, MembershipMatches: true, MembersNonTerminal: true, RequestMatches: true,
	}
}

func sameOwner(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}

	return *left == *right
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
	db pgxdb.Querier,
	requested []requestedLogicalGroup,
) ([]transitionRow, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	return queryLogicalGroupRows(ctx, db, s.logicalGroupArgs(requested))
}

func queryLogicalGroupRows(ctx context.Context, db pgxdb.Querier, args []any) ([]transitionRow, error) {
	var rows []transitionRow

	if err := dbx.SelectSQL(ctx, db, &rows, "load transition logical group rows", transitionGroupRowsSQL, args...); err != nil {
		return nil, fmt.Errorf("load transition logical group rows: %w", err)
	}

	return rows, nil
}

func (s *TransitionStore) logicalGroupArgs(requested []requestedLogicalGroup) []any {
	ids := make([]int64, 0, len(requested))
	roomIDs := make([]string, 0, len(requested)*3)
	kinds := make([]string, 0, len(requested)*3)
	candidates := make([]string, 0, len(requested)*3)

	for i := range requested {
		ids = append(ids, requested[i].delivery.ID)
		for _, candidate := range requested[i].candidate {
			roomIDs = append(roomIDs, requested[i].key.RoomID)
			kinds = append(kinds, string(requested[i].key.Kind))
			candidates = append(candidates, candidate)
		}
	}

	limit := s.logicalGroupWindowLimit(len(requested))

	return []any{ids, roomIDs, kinds, candidates, limit}
}

func (s *TransitionStore) logicalGroupWindowLimit(requestedCount int) int {
	return requestedCount * (s.config.LogicalGroupLimit + 2)
}

func (s *TransitionStore) logicalGroupWindowComplete(requestedCount, observedCount int) bool {
	return requestedCount == 0 || observedCount < s.logicalGroupWindowLimit(requestedCount)
}

func loadTransitionLedger(
	ctx context.Context,
	db pgxdb.Querier,
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

	if err := dbx.SelectSQL(
		ctx,
		db,
		&records,
		"load transition ledger rows",
		transitionLedgerRowsSQL,
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
	before        transitionRow
	after         transitionRow
	due           lifecycle.DueSpec
	lockFromClock bool
	sentFromClock bool
}

type transitionVectors struct {
	ids              []int64
	expectedStatuses []string
	expectedVersions []int64
	expectedOwners   []*string
	nextStatuses     []string
	nextVersions     []int64
	nextAttempts     []int32
	dueDelaysMS      []*int64
	dueFloors        []*time.Time
	locksFromClock   []bool
	nextOwners       []*string
	sentsFromClock   []bool
	nextSentAts      []*time.Time
	nextErrors       []string
}

func applyRowTransitions(
	ctx context.Context,
	db pgxdb.Querier,
	operation string,
	transitions []rowTransition,
) ([]int64, time.Time, error) {
	return applyFencedRowTransitions(ctx, db, operation, transitions, nil)
}

func applySendingTransitions(
	ctx context.Context,
	db pgxdb.Querier,
	operation string,
	transitions []rowTransition,
	lease time.Duration,
) ([]int64, time.Time, error) {
	return applyFencedRowTransitions(ctx, db, operation, transitions, new(lease.Milliseconds()))
}

func applyFencedRowTransitions(
	ctx context.Context,
	db pgxdb.Querier,
	operation string,
	transitions []rowTransition,
	claimLeaseMS *int64,
) ([]int64, time.Time, error) {
	if len(transitions) == 0 {
		return nil, time.Time{}, nil
	}

	vectors, err := buildTransitionVectors(operation, transitions)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: build transition vectors: %w", operation, err)
	}

	rows, err := pgxdb.Query(ctx, db,
		transitionApplyRowsSQL,
		vectors.ids,
		vectors.expectedStatuses,
		vectors.expectedVersions,
		vectors.expectedOwners,
		vectors.nextStatuses,
		vectors.nextVersions,
		vectors.nextAttempts,
		vectors.dueDelaysMS,
		vectors.dueFloors,
		vectors.locksFromClock,
		vectors.nextOwners,
		vectors.sentsFromClock,
		vectors.nextSentAts,
		vectors.nextErrors,
		claimLeaseMS,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: update rows: %w", operation, err)
	}

	scanned, err := pgx.CollectRows(rows, scanTransitionObservation)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: collect fence observations: %w", operation, err)
	}

	observed := make([]sendfence.Observation, 0, len(scanned))
	for i := range scanned {
		observed = append(observed, scanned[i].observation)
	}

	if err := sendfence.Check(operation, vectors.ids, observed); err != nil {
		return nil, time.Time{}, fmt.Errorf("%s: row fence: %w", operation, err)
	}

	return transitionOutboxIDs(transitions), scanned[0].transitionedAt.UTC(), nil
}

type transitionObservation struct {
	observation    sendfence.Observation
	transitionedAt time.Time
}

func scanTransitionObservation(row pgx.CollectableRow) (transitionObservation, error) {
	var (
		scanned transitionObservation
		o       = &scanned.observation
	)

	if err := row.Scan(
		&o.ID, &o.Status,
		&o.StatusMatches, &o.OwnerMatches, &o.VersionMatches, &o.LeaseValid,
		&o.MembershipMatches, &o.MembersNonTerminal, &o.RequestMatches, &o.Applied,
		&scanned.transitionedAt,
	); err != nil {
		return transitionObservation{}, fmt.Errorf("scan transition observation: %w", err)
	}

	return scanned, nil
}

func buildTransitionVectors(operation string, transitions []rowTransition) (transitionVectors, error) {
	vectors := newTransitionVectors(len(transitions))

	for i := range transitions {
		nextAttempt, err := checkedTransitionAttempt(operation, transitions[i].after)
		if err != nil {
			return transitionVectors{}, fmt.Errorf("%s: validate next attempt: %w", operation, err)
		}

		vectors.appendTransition(transitions[i], nextAttempt)
	}

	return vectors, nil
}

func newTransitionVectors(capacity int) transitionVectors {
	return transitionVectors{
		ids: make([]int64, 0, capacity), expectedStatuses: make([]string, 0, capacity),
		expectedVersions: make([]int64, 0, capacity), expectedOwners: make([]*string, 0, capacity),
		nextStatuses: make([]string, 0, capacity), nextVersions: make([]int64, 0, capacity),
		nextAttempts: make([]int32, 0, capacity), dueDelaysMS: make([]*int64, 0, capacity),
		dueFloors: make([]*time.Time, 0, capacity), locksFromClock: make([]bool, 0, capacity),
		nextOwners: make([]*string, 0, capacity), sentsFromClock: make([]bool, 0, capacity),
		nextSentAts: make([]*time.Time, 0, capacity), nextErrors: make([]string, 0, capacity),
	}
}

func (v *transitionVectors) appendTransition(transition rowTransition, nextAttempt int32) {
	delayMS, floor := dueVectorValues(transition.due)

	v.ids = append(v.ids, transition.before.ID)
	v.expectedStatuses = append(v.expectedStatuses, string(transition.before.Status))
	v.expectedVersions = append(v.expectedVersions, transition.before.RowVersion)
	v.expectedOwners = append(v.expectedOwners, cloneStringPtr(transition.before.LockedBy))
	v.nextStatuses = append(v.nextStatuses, string(transition.after.Status))
	v.nextVersions = append(v.nextVersions, transition.after.RowVersion)
	v.nextAttempts = append(v.nextAttempts, nextAttempt)
	v.dueDelaysMS = append(v.dueDelaysMS, delayMS)
	v.dueFloors = append(v.dueFloors, floor)
	v.locksFromClock = append(v.locksFromClock, transition.lockFromClock)
	v.nextOwners = append(v.nextOwners, cloneStringPtr(transition.after.LockedBy))
	v.sentsFromClock = append(v.sentsFromClock, transition.sentFromClock)
	v.nextSentAts = append(v.nextSentAts, cloneTimePtr(transition.after.SentAt))
	v.nextErrors = append(v.nextErrors, deliverysql.TruncateString(transition.after.Error, 500))
}

func dueVectorValues(due lifecycle.DueSpec) (*int64, *time.Time) {
	if due.Mode != lifecycle.DueDelay {
		return nil, nil
	}

	delayMS := new(due.Delay.Milliseconds())
	if due.Floor.IsZero() {
		return delayMS, nil
	}

	return delayMS, new(due.Floor.UTC())
}

func checkedTransitionAttempt(operation string, row transitionRow) (int32, error) {
	if row.AttemptCount < math.MinInt32 || row.AttemptCount > math.MaxInt32 {
		return 0, fmt.Errorf("%s: delivery %d attempt count %d exceeds database integer range", operation, row.ID, row.AttemptCount)
	}

	return int32(row.AttemptCount), nil
}

func (s *TransitionStore) executeTx(
	ctx context.Context,
	operation string,
	fn func(tx pgxdb.Querier) error,
) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%s: begin transaction: %w", operation, err)
	}

	if err := fn(tx); err != nil {
		rollbackErr := pgxdb.Rollback(ctx, tx)
		if rollbackErr != nil {
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

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
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
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

	// afterCommit은 PostgreSQL의 COMMIT 성공 뒤 응답 유실과 재조회를 재현하는 비공개 테스트 훅이다.
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

	return &TransitionStore{db: deliveryDB, logger: logger, config: config}, nil
}

// ClaimPending은 claim 가능한 PENDING 행을 이 인스턴스 소유로 잠그며, lease 만료·due·신선도 판정과 새 locked_at을
// 모두 DB 시계로 정한다.
func (s *TransitionStore) ClaimPending(ctx context.Context, batchSize int) ([]domain.YouTubeNotificationDelivery, error) {
	if batchSize <= 0 {
		return nil, errors.New("claim pending: batch size must be positive")
	}

	rows, err := s.db.Query(
		ctx,
		claimPendingSQL,
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

// resolveClaimedGroups는 호출자 claim 행의 논리 그룹을 같은 트랜잭션에서 잠가 다시 읽고 해석한다. 다시 읽은 행이 호출자
// claim과 다르면 해석 전에 operation 이름의 *sendfence.MissError를 돌려준다.
func (s *TransitionStore) resolveClaimedGroups(
	ctx context.Context,
	db dbx.Querier,
	operation string,
	claimed []domain.YouTubeNotificationDelivery,
	outboxByID map[int64]domain.YouTubeNotificationOutbox,
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

	// 그룹 행 조회의 LIMIT에 claim 행이 잘리면 그 그룹은 반드시 키당 한도를 넘으므로, 누락(missing)으로 오판하지 않게
	// 한도 검사를 fence보다 먼저 한다.
	rowsByID, snapshots, err := s.claimedResolutionSnapshots(rows, currentIDs, requestedSet)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: build snapshots: %w", err)
	}

	if fenceErr := checkClaimFence(operation, claimed, rows); fenceErr != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: claim fence: %w", fenceErr)
	}

	ledgerByKey, evidence, err := loadTransitionLedger(ctx, db, requestedKeys)
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: load ledger: %w", err)
	}

	resolver, err := preparation.NewResolver(preparation.ResolverConfig{
		LogicalGroupScanLimit: s.config.LogicalGroupLimit,
		LockTimeout:           s.config.LockTimeout,
		RequireTerminalLedger: true,
	})
	if err != nil {
		return resolvedLogicalGroups{}, fmt.Errorf("resolve claimed groups: resolver: %w", err)
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

	return key, row.snapshot(current), true, nil
}

// checkClaimFence는 잠가 다시 읽은 행이 호출자 claim과 같은 세대인지 본다. 상태를 PENDING으로 둔 채 소유자와 row_version을
// 바꾸는 것이 claim이므로 셋이 모두 같아야 호출자가 아직 그 행을 소유한다. 하나라도 어긋나면 대상 전원을 미적용으로 담은
// 미스를 돌려주며, 호출자는 이 트랜잭션에서 아무 행도 바꾸지 않는다.
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
	db dbx.Querier,
	requested []requestedLogicalGroup,
) ([]transitionRow, error) {
	if len(requested) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(requested))
	roomIDs := make([]string, 0, len(requested)*3)
	kinds := make([]string, 0, len(requested)*3)
	candidates := make([]string, 0, len(requested)*3)

	for i := range requested {
		ids = append(ids, requested[i].delivery.ID)
		// 배열의 같은 위치가 한 요청 조합입니다. 개별 중복 제거는 교차 조합까지 잠그게 합니다.
		for _, candidate := range requested[i].candidate {
			roomIDs = append(roomIDs, requested[i].key.RoomID)
			kinds = append(kinds, string(requested[i].key.Kind))
			candidates = append(candidates, candidate)
		}
	}

	limit := len(requested)*(s.config.LogicalGroupLimit+1) + len(ids)

	var rows []transitionRow

	if err := dbx.SelectSQL(
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

	if err := dbx.SelectSQL(
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

// rowTransition의 after는 시각 열을 뺀 다음 상태다. 시각 열은 전이 문장이 대상을 잠근 뒤 채취한 DB 시계로 정하며,
// next_attempt_at은 due를, locked_at은 lockFromClock이면 그 시계를 아니면 NULL을, sent_at은 sentFromClock이면 그
// 시계를 아니면 after.SentAt(DB에서 읽은 ledger sent_at 또는 nil)을 쓴다.
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

// applyRowTransitions는 before의 (status, locked_by, row_version) fence로 대상 전원을 함께 전이한다. 한 대상이라도
// fence가 어긋나면 아무 행도 바꾸지 않고 *sendfence.MissError를 돌려준다. 성공하면 전이 시각(DB 시계)을 함께 돌려준다.
func applyRowTransitions(
	ctx context.Context,
	db dbx.Querier,
	operation string,
	transitions []rowTransition,
) ([]int64, time.Time, error) {
	return applyFencedRowTransitions(ctx, db, operation, transitions, nil)
}

// applySendingTransitions는 외부 발송 권한을 만드는 전이를 적용한다. 정지했던 소유자가 lease 밖에서 발송을 시작하지
// 않도록 행 fence에 더해 claim lease(locked_at + lease)가 DB 시계 기준으로 살아 있어야 한다.
func applySendingTransitions(
	ctx context.Context,
	db dbx.Querier,
	operation string,
	transitions []rowTransition,
	lease time.Duration,
) ([]int64, time.Time, error) {
	return applyFencedRowTransitions(ctx, db, operation, transitions, new(lease.Milliseconds()))
}

func applyFencedRowTransitions(
	ctx context.Context,
	db dbx.Querier,
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

	rows, err := db.Query(
		ctx,
		mustSQL("transition_apply_rows.sql"),
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

	// fence를 통과했으면 대상 전원이 관측되었으므로 첫 행의 전이 시각이 문장 전체의 시계다.
	return transitionOutboxIDs(transitions), scanned[0].transitionedAt.UTC(), nil
}

// transitionObservation은 전이 SQL 한 행의 sendfence 관측 열과 그 뒤에 붙은 전이 시각이다.
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

// dueVectorValues는 DueSpec을 전이 SQL의 (지연 ms, 하한) 쌍으로 바꾼다. 유지는 지연 NULL, 하한이 없으면 하한 NULL이다.
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
	fn func(tx dbx.Querier) error,
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

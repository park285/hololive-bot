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

	// CleanupGuardIncompleteLogicalGroup은 경쟁 트랜잭션이 잠근 행, LogicalGroupLimit 초과, 논리 키별 조회 한도 도달
	// 때문에 후보의 자식이나 같은 논리 그룹을 모두 잠가 관측하지 못한 후보다. 기다리지 않고 이번 batch에서 보존하며,
	// 같은 batch의 다른 논리 키 후보 판정에는 영향을 주지 않는다.
	CleanupGuardIncompleteLogicalGroup CleanupGuardReason = "incomplete_logical_group"
)

// CleanupCursor는 보호된 outbox를 넘어 진행해 오래된 그룹이 후속 정리를 막지 않게 한다.
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

// CleanupExpiredFanoutOutboxes는 pendingAge보다 오래되고 claim lease가 끝난 미전개 PENDING outbox를 지운다.
// 두 판정 모두 DB 시계 기준이다.
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
		mustSQL("fanout_cleanup_expired.sql"),
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

// cleanupObservedRow는 cleanup이 기다리지 않고 잠그려 한 행이다. Locked가 false면 경쟁 트랜잭션이 잠가 건너뛴 행이고,
// 값은 문장 snapshot이라 상태 판정에 쓰지 않으며 소속 outbox와 논리 키로 불완전 관측을 표시하는 데만 쓴다.
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
		mustSQL("transition_cleanup_children.sql"),
		outboxIDs,
	); err != nil {
		return nil, fmt.Errorf("load terminal outbox cleanup children: %w", err)
	}

	return rows, nil
}

// cleanupEligibleOutboxes는 자식과 같은 논리 그룹을 모두 잠가 관측한 후보만 ledger·sibling guard로 판정한다.
// 후보 부모·자식을 쥔 채 경쟁 트랜잭션의 행을 기다리지 않으므로, 불완전 관측 후보는 삭제하지 않고 보호로 센다.
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

	// 완전 관측한 논리 키의 delivery 행은 모두 이 트랜잭션이 잠갔다. ledger writer(발송 완료·격리)는 같은 키의
	// delivery 행을 먼저 잠근 뒤 ledger를 잠그므로, 완전 관측한 키만 조회하면 그 writer를 기다리지 않는다.
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

// cleanupInputs는 자식을 모두 잠근 후보의 자식만 논리 그룹 요청으로 만든 결과다. 잠그지 못한 자식이 있는 후보는
// uncovered에만 남고 sibling·ledger 조회에서 빠진다.
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

// covers는 후보의 자식과 그 자식들의 논리 그룹을 모두 잠가 관측했는지 답한다. 필드 incomplete는 잠그지 못한 행,
// LogicalGroupLimit 초과, 논리 키별 조회 한도 도달이 있는 논리 키다. 자식이 없는 후보는 판정할 논리 그룹이 없다.
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

// cleanupGroupRow는 transition_cleanup_group_rows.sql 행이다. SaturatedKeys는 이 행을 한도번째 행으로 남긴 논리 키
// 번호(1부터)이고, 그 키의 identity 조합에는 한도 밖 행이 더 있을 수 있다.
type cleanupGroupRow struct {
	cleanupObservedRow

	SaturatedKeys []int64
}

// cleanupIdentityRequest는 요청 identity 후보를 그 후보를 만든 논리 키 번호에 묶는다. 후보 값이 같아도 키가 다르면
// 다른 요청으로 보내, 찾은 행이 그 후보를 만든 키의 한도를 쓰게 한다.
type cleanupIdentityRequest struct {
	keyNo     int64
	candidate string
}

// cleanupGroupArgs는 transition_cleanup_group_rows.sql의 bind 값이다. 필드 roomIDs·kinds·candidates·keyNos의 같은 위치가
// 한 요청 identity 조합과 그 조합을 만든 논리 키 번호다. 키 번호 n은 keys[n-1]이고 keyNoByKey는 그 역방향이다.
type cleanupGroupArgs struct {
	ids        []int64
	roomIDs    []string
	kinds      []string
	candidates []string
	keyNos     []int64
	keys       []ytcontentid.LogicalKey
	keyNoByKey map[ytcontentid.LogicalKey]int64
}

// newCleanupGroupArgs는 요청 논리 키마다 번호를 붙이고, logicalIdentityCandidates가 만든 조합은 (키 번호, 후보)마다
// 한 번씩만 보낸다. 같은 키의 자식이 여럿이면 조합이 겹치므로, 중복을 빼야 같은 조합을 다시 훑지 않는다.
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

// loadCleanupSiblings는 요청 논리 그룹 행을 경쟁 잠금을 기다리지 않고 잠근다. 조회 한도는 batch 전체가 아니라 요청 논리
// 키마다 LogicalGroupLimit+1행이고, 반환 행은 최대 len(requested) + 요청 키 수×(LogicalGroupLimit+1)행이다. 잠근 요청
// 키 행만 sibling 판정에 돌려준다. 잠그지 못한 행·LogicalGroupLimit 초과 행이 있는 키와 한도에 닿은 키는 그 키만
// incomplete로 돌려, 다른 요청 키의 판정은 막지 않는다. 키의 조합이 다른 키로 풀리는 행을 찾아 한도에 닿아도 그 조합을
// 만든 키를 막는다. 논리 키를 풀 수 없는 행은 기존 sibling 판정처럼 어느 요청 그룹에도 속하지 않는다.
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
		mustSQL("transition_cleanup_group_rows.sql"),
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

		// FAILED는 provider 미전달이 확인된 상태라 ledger가 보존하지 않습니다(L-001).
		// 계약은 FAILED owner 삭제에 같은 논리 키의 PENDING/SENDING sibling 부재만 요구하므로
		// ledger 증거를 요구하면 FAILED 자식이 있는 outbox가 영구히 남습니다.
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

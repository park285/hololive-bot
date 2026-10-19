package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/dbx"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

// handoffUnit은 고정 멤버(이 발송의 owner) 하나와 그 논리 키의 잠근 delivery 전부다. 필드 rows에는 owner와 고정 멤버 밖의
// 중복 물리행(follower)이 함께 있다.
type handoffUnit struct {
	owner transitionRow
	key   ytcontentid.LogicalKey
	rows  []transitionRow
}

// handoffMembers는 handoff_members.sql로 읽은 고정 멤버와 그 논리 키다. 논리 키를 풀 수 없는 멤버는 keyByID와 그룹
// 잠금 요청(requested)에 들지 않고, ownersByKey는 논리 키마다 그 키를 가진 고정 멤버 수다.
type handoffMembers struct {
	byID        map[int64]transitionRow
	keyByID     map[int64]ytcontentid.LogicalKey
	ownersByKey map[ytcontentid.LogicalKey]int
	requested   []requestedLogicalGroup
}

// handoffGroup은 고정 멤버의 논리 그룹을 잠근 결과다. 필드 parentIDs는 사전 잠근 부모 outbox id(오름차순)이고 rows는
// 잠근 그룹 delivery다. 필드 complete가 거짓이면 조회 창 밖 형제의 유무를 증명하지 못한다.
type handoffGroup struct {
	parentIDs []int64
	rows      []transitionRow
	complete  bool
}

// lockHandoffUnits는 고정 멤버의 논리 그룹이 걸친 부모 outbox와 그 그룹 delivery를 잠그고(lockHandoffGroup) 멤버마다
// sendfence 관측을 만든다. 잠근 그룹 행의 부모가 사전 잠금 집합 밖이면(그 사이 그룹에 새 follower·outbox가 들어옴)
// 원장을 잠그지 않고 handoffGroupGrewMiss의 미스 오류로 끝낸다. 그 밖의 관측은 다음과 같다.
//   - status_matches: 멤버가 SENT다.
//   - version_matches: 멤버가 claim 때 이 요청에 묶인 SENT였고 row_version이 그대로다.
//   - membership_matches: 멤버의 논리 단위가 이 admission이 만든 그대로다. 논리 키를 풀 수 있고, 다른 고정 멤버와
//     키가 겹치지 않고, 그룹이 한도 안이라 전부 잠갔고, 그룹에 SENDING·QUARANTINED 행이 없으며, 그 키의 원장이 이
//     멤버를 출처로 한 SENT다.
//
// 원장 출처를 뺀 관측은 잠근 delivery만으로 정해진다. 그중 하나라도 어긋나면 원장 값과 상관없이 호출자가 확인을 닫으므로
// 원장을 잠그지 않고, 전원이 맞을 때만 원장을 잠가 출처를 확인한다(observeHandoffLedger). 소유자 없는 정산이라
// owner_matches·lease_valid, 요청 fence는 앞 문장이 확인했으므로 request_matches와 members_non_terminal은 TRUE다.
// 전원이 맞을 때만 applied다.
func (s *TransitionStore) lockHandoffUnits(
	ctx context.Context,
	tx dbx.Querier,
	action string,
	claim *HandoffClaim,
) ([]handoffUnit, []sendfence.Observation, error) {
	members, err := loadHandoffMembers(ctx, tx, claim.MemberIDs)
	if err != nil {
		return nil, nil, err
	}

	group, err := s.lockHandoffGroup(ctx, tx, members.requested)
	if err != nil {
		return nil, nil, err
	}

	if miss := handoffGroupGrewMiss(action, claim, members, group); miss != nil {
		return nil, nil, miss
	}

	units, observed := s.observeHandoffUnits(claim, members, group.rows, group.complete)

	if handoffObservationsMatch(claim, observed) {
		if observeErr := observeHandoffLedger(ctx, tx, units, observed); observeErr != nil {
			return nil, nil, observeErr
		}
	}

	applied := handoffObservationsMatch(claim, observed)
	for i := range observed {
		observed[i].Applied = applied
	}

	return units, observed, nil
}

// loadHandoffMembers는 고정 멤버를 잠그지 않고 읽어 논리 키와 그룹 잠금 요청을 만든다.
func loadHandoffMembers(ctx context.Context, tx dbx.Querier, memberIDs []int64) (handoffMembers, error) {
	var rows []transitionRow

	if err := dbx.SelectSQL(ctx, tx, &rows, "load handoff members", mustSQL("handoff_members.sql"), memberIDs); err != nil {
		return handoffMembers{}, fmt.Errorf("load frozen members: %w", err)
	}

	members := handoffMembers{
		byID:        make(map[int64]transitionRow, len(rows)),
		keyByID:     make(map[int64]ytcontentid.LogicalKey, len(rows)),
		ownersByKey: make(map[ytcontentid.LogicalKey]int, len(rows)),
		requested:   make([]requestedLogicalGroup, 0, len(rows)),
	}

	for i := range rows {
		members.byID[rows[i].ID] = rows[i]

		key, keyErr := rows[i].logicalKey()
		if keyErr != nil {
			continue
		}

		members.keyByID[rows[i].ID] = key
		members.ownersByKey[key]++

		outbox := rows[i].domainOutbox()

		members.requested = append(members.requested, requestedLogicalGroup{
			key: key, delivery: rows[i].domainDelivery(), outbox: outbox,
			candidate: logicalIdentityCandidates(key, outbox),
		})
	}

	return members, nil
}

// lockHandoffGroup은 고정 멤버의 논리 그룹이 걸친 부모 outbox를 먼저 잠그고(lockHandoffParents), 그 그룹 delivery를
// transition_group_rows.sql(다른 그룹 해석 경로와 같은 순서)로 잠근다. 논리 키를 풀 수 있는 멤버가 없으면 잠글 그룹도
// 없으며, 그 멤버들은 관측에서 membership 미스가 된다.
func (s *TransitionStore) lockHandoffGroup(ctx context.Context, tx dbx.Querier, requested []requestedLogicalGroup) (handoffGroup, error) {
	if len(requested) == 0 {
		return handoffGroup{}, nil
	}

	groupArgs := s.logicalGroupArgs(requested)

	parentIDs, err := lockHandoffParents(ctx, tx, groupArgs)
	if err != nil {
		return handoffGroup{}, err
	}

	rows, err := queryLogicalGroupRows(ctx, tx, groupArgs)
	if err != nil {
		return handoffGroup{}, fmt.Errorf("lock logical group rows: %w", err)
	}

	return handoffGroup{
		parentIDs: parentIDs, rows: rows,
		complete: s.logicalGroupWindowComplete(len(requested), len(rows)),
	}, nil
}

// lockHandoffParents는 논리 그룹 delivery를 잠그기 전에 그 그룹이 걸친 부모 outbox id를 그룹 잠금과 같은 후보 조각·인자
// (handoff_group_parents.sql, logicalGroupArgs)로 잠그지 않고 읽어 canonical 집계 잠금(lockOutboxAggregateParents)으로
// id 순서로 잠근다. 돌려준 id는 오름차순이다. 이미 지워진 부모는 잠금 결과에 없고 그 자식도 함께 지워졌으므로 뒤의 그룹
// 잠금에 나오지 않는다.
func lockHandoffParents(ctx context.Context, tx dbx.Querier, groupArgs []any) ([]int64, error) {
	var parentIDs []int64

	if err := dbx.SelectSQL(ctx, tx, &parentIDs, "load handoff group parents", handoffGroupParentsSQL, groupArgs...); err != nil {
		return nil, fmt.Errorf("load logical group parents: %w", err)
	}

	if err := lockOutboxAggregateParents(ctx, tx, parentIDs); err != nil {
		return nil, err
	}

	return parentIDs, nil
}

// handoffGroupGrewMiss는 잠근 그룹 행의 부모가 모두 사전 잠금 집합(group.parentIDs)에 있으면 nil이다. 사전 조회 뒤
// 그룹에 새 follower·outbox가 들어와 집합이 늘었으면, 자식 잠금을 쥔 채 새 부모를 더 잠그면 부모 → 자식 순서인 보존
// 정리·fanout과 순환 대기가 생기므로 잠그지 않는다. 대신 그 논리 키의 고정 멤버(키로 가릴 수 없으면 전원)를
// membership_matches 거짓으로 관측한 *sendfence.MissError를 감싸 돌려준다. 호출자는 이를 오류로 돌려 정산 전체를
// 되돌리고 확인을 닫지 않으므로(ErrHandoffCheckClosed 아님), claim이 미룬 soft claim 뒤 다음 확인이 늘어난 집합으로
// 다시 정산한다.
func handoffGroupGrewMiss(action string, claim *HandoffClaim, members handoffMembers, group handoffGroup) error {
	grown := make(map[ytcontentid.LogicalKey]struct{})
	unkeyed := false

	for i := range group.rows {
		if _, prelocked := slices.BinarySearch(group.parentIDs, group.rows[i].OutboxID); prelocked {
			continue
		}

		key, keyErr := group.rows[i].logicalKey()
		if keyErr != nil {
			unkeyed = true

			continue
		}

		grown[key] = struct{}{}
	}

	if len(grown) == 0 && !unkeyed {
		return nil
	}

	changed := make(map[int64]bool, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		key, keyed := members.keyByID[id]
		if _, hit := grown[key]; keyed && hit {
			changed[id] = true
		}
	}

	observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		observed = append(observed, sendfence.Observation{
			ID: id, Status: string(members.byID[id].Status),
			StatusMatches: true, OwnerMatches: true, VersionMatches: true, LeaseValid: true,
			MembershipMatches: len(changed) > 0 && !changed[id], MembersNonTerminal: true, RequestMatches: true,
		})
	}

	if err := sendfence.Check(action, claim.MemberIDs, observed); err != nil {
		return fmt.Errorf("logical group gained parents: %w", err)
	}

	return nil
}

// observeHandoffUnits는 고정 멤버마다 논리 단위와 원장 출처를 뺀 sendfence 관측을 같은 순서로 만든다. 두 결과의 i번째는
// 같은 멤버이고, 잠근 행도 읽은 행도 없는 멤버는 둘 다에서 빠진다.
func (s *TransitionStore) observeHandoffUnits(
	claim *HandoffClaim,
	members handoffMembers,
	locked []transitionRow,
	complete bool,
) ([]handoffUnit, []sendfence.Observation) {
	rowsByID, rowsByKey := indexHandoffGroupRows(locked, members.ownersByKey)
	units := make([]handoffUnit, 0, len(claim.MemberIDs))
	observed := make([]sendfence.Observation, 0, len(claim.MemberIDs))

	for _, id := range claim.MemberIDs {
		row, ok := rowsByID[id]
		rowLocked := ok

		if !ok {
			// 잠금 창에서 빠진 멤버는 읽은 행으로 미스 관측만 남기며, 이 정보로 정산을 허용하지 않는다.
			if row, ok = members.byID[id]; !ok {
				continue
			}
		}

		key, keyed := members.keyByID[id]
		unitRows := rowsByKey[key]
		version, sent := claim.SentMembers[id]

		observed = append(observed, sendfence.Observation{
			ID: id, Status: string(row.Status),
			StatusMatches: row.Status == lifecycle.StatusSent, OwnerMatches: true,
			VersionMatches: sent && row.RowVersion == version, LeaseValid: true,
			MembershipMatches: complete && rowLocked && keyed && members.ownersByKey[key] == 1 && len(unitRows) <= s.config.LogicalGroupLimit &&
				!slices.ContainsFunc(unitRows, unresolvedOrInFlight),
			MembersNonTerminal: true, RequestMatches: true,
		})
		units = append(units, handoffUnit{owner: row, key: key, rows: unitRows})
	}

	return units, observed
}

// indexHandoffGroupRows는 잠근 그룹 행을 id별로, 그리고 고정 멤버가 가진 논리 키별로 모은다.
func indexHandoffGroupRows(
	locked []transitionRow,
	ownersByKey map[ytcontentid.LogicalKey]int,
) (map[int64]transitionRow, map[ytcontentid.LogicalKey][]transitionRow) {
	rowsByID := make(map[int64]transitionRow, len(locked))
	rowsByKey := make(map[ytcontentid.LogicalKey][]transitionRow, len(ownersByKey))

	for i := range locked {
		rowsByID[locked[i].ID] = locked[i]

		key, keyErr := locked[i].logicalKey()
		if keyErr != nil {
			continue
		}

		if _, wanted := ownersByKey[key]; wanted {
			rowsByKey[key] = append(rowsByKey[key], locked[i])
		}
	}

	return rowsByID, rowsByKey
}

// handoffObservationsMatch는 고정 멤버 전원이 관측됐고 그 상태·행 버전·membership이 모두 맞는지다.
func handoffObservationsMatch(claim *HandoffClaim, observed []sendfence.Observation) bool {
	return len(observed) == len(claim.MemberIDs) && !slices.ContainsFunc(observed, func(o sendfence.Observation) bool {
		return !o.StatusMatches || !o.VersionMatches || !o.MembershipMatches
	})
}

// observeHandoffLedger는 로컬 관측이 모두 맞은 논리 단위의 원장을 transition_ledger_rows.sql(키 순서, FOR UPDATE)로
// 잠그고, 그 키의 원장이 owner를 출처로 한 SENT인지를 membership_matches에 더한다. 관측 observed[i]는 units[i]의 것이다.
func observeHandoffLedger(ctx context.Context, tx dbx.Querier, units []handoffUnit, observed []sendfence.Observation) error {
	keys := make([]ytcontentid.LogicalKey, 0, len(units))
	for i := range units {
		keys = append(keys, units[i].key)
	}

	ledgerByKey, _, err := loadTransitionLedger(ctx, tx, keys)
	if err != nil {
		return fmt.Errorf("lock logical ledger: %w", err)
	}

	for i := range observed {
		observed[i].MembershipMatches = observed[i].MembershipMatches &&
			ledgerSourcedBy(ledgerByKey, units[i].key, units[i].owner.ID)
	}

	return nil
}

func ledgerSourcedBy(ledgerByKey map[ytcontentid.LogicalKey]DeliveryLedgerRecord, key ytcontentid.LogicalKey, ownerID int64) bool {
	record, ok := ledgerByKey[key]

	return ok && record.Status == LedgerStatusSent && record.SourceDeliveryID != nil && *record.SourceDeliveryID == ownerID
}

func unresolvedOrInFlight(row transitionRow) bool {
	return row.Status == lifecycle.StatusSending || row.Status == lifecycle.StatusQuarantined
}

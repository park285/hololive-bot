// Package sendfence는 외부 발송 원장(alarm-dispatch, notification-delivery, youtube)의 소유자 전이가 함께 쓰는 fence
// 규약이다. 전이 SQL이 대상 행을 잠근 같은 문장에서 계산한 조건값(Observation)을 받아 미스 원인(Reason)을 분류하고,
// 적용되지 않은 대상이 하나라도 있으면 *MissError를 돌려준다. 테이블별 SQL·재시도·미스 라우팅은 각 원장이 소유한다.
//
// 전이 SQL 규약:
//
//  1. locked: 대상 전원을 id 순서로 FOR UPDATE 잠근다. 없는 행은 관측 행도 없으며 Check가 ReasonMissing으로 분류한다.
//  2. clock: locked 집계(count)에 의존하는 MATERIALIZED CTE에서 clock_timestamp()를 한 번만 채취한다. 그래서 시각은
//     모든 잠금이 끝난 뒤의 값이다. lease 만료 판정과 기록하는 시각(*_at, lock_expires_at, next_attempt_at)은 모두 이
//     값에서 계산한다. Go는 시각 대신 lease_ms·retry_after_ms 같은 밀리초 정수만 넘긴다.
//  3. checked: 조건을 NULL 없는 boolean 열로 계산한다. 그 전이에 해당하지 않는 조건은 TRUE다.
//  4. 갱신: 조건이 모두 참인 대상만 UPDATE하고 RETURNING id로 적용 여부를 남긴다. 단위 전원이 함께 전이해야 하는
//     문장은 단위 전원의 조건이 참일 때만 갱신한다.
//  5. 관측: 잠근 대상마다 ScanObservation 순서의 열을 돌려준다. 열 이름은 원장 어휘를 따라도 되지만 순서는 고정이다.
//     id, status, status_matches, owner_matches, version_matches, lease_valid, membership_matches,
//     members_non_terminal, request_matches, applied
//  6. next_attempt_at은 GREATEST(clock + retry_after_ms * 1ms, floor)로 계산한다. floor는 DB 행에서 온 timestamptz만
//     쓴다. 기존 값을 유지할 때는 SET에서 그 열을 그대로 둔다.
//
// 열 의미:
//
//   - version_matches는 원장의 claim 세대 열 등가다. alarm-dispatch·notification은 attempt_count, youtube는
//     row_version이다. attempt_count처럼 claim에서 바뀌지 않는 열이면 같은 소유자 이름의 재claim을 구분하지 못하므로,
//     그 원장이 재claim 직렬성을 따로 보장한다.
//   - lease_valid는 원장이 만료를 계산한다(lock_expires_at > clock 또는 locked_at + lease_ms > clock). 외부 발송 뒤
//     정산처럼 외부 결과를 lease 만료로 되돌리지 않는 전이는 TRUE다.
//   - membership_matches는 고정된 단위 멤버 집합과 현재 멤버·호출 대상 집합의 등가, members_non_terminal은 고정 멤버
//     중 종료 상태가 없음이다. 단일 행 원장은 둘 다 TRUE다.
//   - request_matches는 고정 요청의 세대·식별자 등가다. 요청 본문 전체 JSON 등가는 쓰지 않는다.
package sendfence

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reason은 fence 미스의 관측 원인이다. 한 대상에서 여러 조건이 어긋나면 missing, status, owner, version, lease,
// member_terminal, membership, request 순서로 첫 원인 하나만 기록한다.
type Reason string

const (
	// ReasonMissing은 대상 행이 없다는 뜻이다. 다른 경로가 끝낸 행을 보존 정리가 지운 경우다.
	ReasonMissing Reason = "missing"
	// ReasonStatusChanged는 전이가 허용하는 상태가 아니라는 뜻이다. 다른 정산이나 회수·격리 경로가 앞선 경우다.
	ReasonStatusChanged Reason = "status_changed"
	// ReasonOwnerChanged는 호출자가 소유자가 아니라는 뜻이다. 다른 소유자가 lease 만료 뒤 재claim했거나 전이가 소유자를
	// 비운 경우다.
	ReasonOwnerChanged Reason = "owner_changed"
	// ReasonVersionChanged는 claim 세대 열이 호출자 claim과 다르다는 뜻이다. 같은 소유자 이름으로 다른 claim 세대가
	// 정산되거나 재claim된 경우다.
	ReasonVersionChanged Reason = "version_changed"
	// ReasonLeaseExpired는 발송 전 claim의 lease가 만료됐다는 뜻이다. 아직 재claim되지 않았어도 회수 경로가 가져간다.
	ReasonLeaseExpired Reason = "lease_expired"
	// ReasonMemberTerminal은 고정 단위 멤버 중 종료 상태가 있어 단위를 다시 모을 수 없다는 뜻이다.
	ReasonMemberTerminal Reason = "member_terminal"
	// ReasonMembershipChanged는 고정 단위 멤버 집합과 현재 멤버·호출 대상 집합이 다르다는 뜻이다.
	ReasonMembershipChanged Reason = "membership_changed"
	// ReasonRequestChanged는 고정 요청의 세대·식별자가 호출자가 기대한 값과 다르다는 뜻이다.
	ReasonRequestChanged Reason = "request_changed"
	// ReasonPeerMissed는 이 대상의 조건은 모두 맞지만 단위 전원이 함께 전이하는 문장에서 다른 대상이 미스여서 함께
	// 적용되지 않았다는 뜻이다.
	ReasonPeerMissed Reason = "peer_missed"
)

// OwnershipHeld는 이 원인으로 적용되지 않은 뒤에도 호출자가 행을 소유하는지 보고한다. 상태·소유자·세대·lease가 모두
// 맞은 미스만 참이며, 원장은 이 행에만 소유자 fence가 걸린 후속 전이(해제·실패·격리)를 쓸 수 있다.
func (r Reason) OwnershipHeld() bool {
	switch r {
	case ReasonMemberTerminal, ReasonMembershipChanged, ReasonRequestChanged, ReasonPeerMissed:
		return true
	case ReasonMissing, ReasonStatusChanged, ReasonOwnerChanged, ReasonVersionChanged, ReasonLeaseExpired:
		return false
	}

	return false
}

// Observation은 전이 문장이 잠근 대상 하나에서 같은 clock으로 계산해 돌려준 조건값이다. 각 조건은 참이면 fence가 맞다는
// 뜻이고 Applied는 그 문장이 이 대상을 실제로 갱신했는지다.
type Observation struct {
	ID                 int64
	Status             string
	StatusMatches      bool
	OwnerMatches       bool
	VersionMatches     bool
	LeaseValid         bool
	MembershipMatches  bool
	MembersNonTerminal bool
	RequestMatches     bool
	Applied            bool
}

// ScanObservation은 패키지 규약 순서의 관측 열 한 행을 읽으며 pgx.CollectRows에 넘겨 쓴다.
func ScanObservation(row pgx.CollectableRow) (Observation, error) {
	var o Observation

	err := row.Scan(
		&o.ID, &o.Status,
		&o.StatusMatches, &o.OwnerMatches, &o.VersionMatches, &o.LeaseValid,
		&o.MembershipMatches, &o.MembersNonTerminal, &o.RequestMatches, &o.Applied,
	)
	if err != nil {
		return Observation{}, fmt.Errorf("scan send fence observation: %w", err)
	}

	return o, nil
}

// reason은 적용되지 않은 관측의 첫 원인이다.
func (o *Observation) reason() Reason {
	switch {
	case !o.StatusMatches:
		return ReasonStatusChanged
	case !o.OwnerMatches:
		return ReasonOwnerChanged
	case !o.VersionMatches:
		return ReasonVersionChanged
	case !o.LeaseValid:
		return ReasonLeaseExpired
	case !o.MembersNonTerminal:
		return ReasonMemberTerminal
	case !o.MembershipMatches:
		return ReasonMembershipChanged
	case !o.RequestMatches:
		return ReasonRequestChanged
	default:
		return ReasonPeerMissed
	}
}

// RowMiss는 적용되지 않은 대상 하나다. Status는 관측한 행 상태이며 행이 없으면 빈 값이다.
type RowMiss struct {
	ID     int64
	Reason Reason
	Status string
}

// MissError는 fence 미스로 요청 대상 중 일부 또는 전부가 적용되지 않았다는 오류다. 적용된 대상은 전이를 마쳤고, Rows의
// 대상은 이 문장이 바꾸지 않았다. 소유권이 없는 대상(Reason.OwnershipHeld가 거짓)은 현재 소유자·회수 경로의 것이므로
// 호출자가 덮어쓰면 안 된다.
type MissError struct {
	Action   string
	Expected int
	Applied  int
	Rows     []RowMiss
}

func (m *MissError) Error() string {
	var rows strings.Builder

	for i, row := range m.Rows {
		if i > 0 {
			rows.WriteString(", ")
		}

		fmt.Fprintf(&rows, "%d=%s", row.ID, row.Reason)

		if row.Status != "" {
			fmt.Fprintf(&rows, "(%s)", row.Status)
		}
	}

	return fmt.Sprintf("%s: send fence miss: applied %d of %d: %s", m.Action, m.Applied, m.Expected, rows.String())
}

// UnappliedDeliveryIDs는 이 문장이 바꾸지 않은 대상 ID를 요청 순서로 돌려준다.
func (m *MissError) UnappliedDeliveryIDs() []int64 {
	ids := make([]int64, 0, len(m.Rows))
	for _, row := range m.Rows {
		ids = append(ids, row.ID)
	}

	return ids
}

// Has는 적용되지 않은 대상 중 reason으로 분류된 것이 있는지 보고한다.
func (m *MissError) Has(reason Reason) bool {
	for _, row := range m.Rows {
		if row.Reason == reason {
			return true
		}
	}

	return false
}

// ErrGenerationExhausted는 고정 요청의 재발급 세대를 모두 써서 새 요청 ID를 만들 수 없다는 뜻이다. 이것은 fence 미스가
// 아니라 정책 종료이며 원장이 종료 전이로 처리한다.
var ErrGenerationExhausted = errors.New("send request generations exhausted")

// Check는 want의 각 대상을 같은 문장의 관측값과 맞춰 분류한다. 전원이 적용됐으면 nil, 아니면 적용되지 않은 대상을
// want 순서로 담은 *MissError다. 중복 없는 ID 목록을 want로 받고, want에 없는 관측은 무시한다.
func Check(action string, want []int64, observed []Observation) error {
	byID := make(map[int64]*Observation, len(observed))
	for i := range observed {
		byID[observed[i].ID] = &observed[i]
	}

	miss := MissError{Action: action, Expected: len(want)}

	for _, id := range want {
		observation, ok := byID[id]

		switch {
		case !ok:
			miss.Rows = append(miss.Rows, RowMiss{ID: id, Reason: ReasonMissing})
		case observation.Applied:
			miss.Applied++
		default:
			miss.Rows = append(miss.Rows, RowMiss{ID: id, Reason: observation.reason(), Status: observation.Status})
		}
	}

	if len(miss.Rows) == 0 {
		return nil
	}

	return &miss
}

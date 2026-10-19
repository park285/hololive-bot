package store

import (
	"embed"
	"strings"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustReader(sqlAssets, "queries")

var (
	claimPendingSQL  = strings.Replace(mustSQL("transition_claim_pending.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
	readySnapshotSQL = strings.Replace(mustSQL("delivery_ready_snapshot.sql"), "/* ready_predicate */", mustSQL("delivery_ready_predicate.sql"), 1)
)

// handoff 적용 문장은 같은 고정 요청 fence 앞부분(handoff_request_fence.sql)에 전이별 갱신을 이어 붙인다.
var (
	handoffCompleteSQL    = withHandoffRequestFence("handoff_complete.sql")
	handoffRescheduleSQL  = withHandoffRequestFence("handoff_reschedule.sql")
	handoffRequestLockSQL = withHandoffRequestFence("handoff_request_lock.sql")
)

func withHandoffRequestFence(name string) string {
	return strings.Replace(mustSQL(name), "/* handoff_request_fence */", mustSQL("handoff_request_fence.sql"), 1)
}

// 논리 그룹 잠금 문장과 handoff 정산의 부모 사전 조회는 같은 후보 조각(transition_group_candidates.sql)을 쓴다. 그래서
// 사전 조회한 부모 집합과 뒤에 잠근 그룹 행은 동시 추가가 없으면 같은 범위에서 나온다.
var (
	transitionGroupRowsSQL = withGroupCandidates("transition_group_rows.sql")
	handoffGroupParentsSQL = withGroupCandidates("handoff_group_parents.sql")
)

func withGroupCandidates(name string) string {
	return strings.Replace(mustSQL(name), "/* transition_group_candidates */", mustSQL("transition_group_candidates.sql"), 1)
}

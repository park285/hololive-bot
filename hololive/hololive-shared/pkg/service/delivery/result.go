package delivery

import "time"

// SendResult: 발송 결과
type SendResult struct {
	Attempted   int      // claim 획득하여 발송 시도한 room 수
	Sent        int      // 발송 성공 (enqueue 성공)
	Skipped     int      // 이미 claim됨 (기 발송) → skip
	Failed      int      // 발송 실패
	FailedRooms []string // 실패 room ID 목록
}

// Merge: 하위 결과를 현재 결과에 병합
func (r *SendResult) Merge(child SendResult) {
	r.Attempted += child.Attempted
	r.Sent += child.Sent
	r.Skipped += child.Skipped
	r.Failed += child.Failed
	if len(child.FailedRooms) > 0 {
		r.FailedRooms = append(r.FailedRooms, child.FailedRooms...)
	}
}

// 분산 락 / Room별 delivery claim TTL
const (
	DefaultExecutionLockTTL = 15 * time.Minute
	WeeklyDeliveryClaimTTL  = 8 * 24 * time.Hour  // 다음 주 새 weekKey 전까지 유효
	MonthlyDeliveryClaimTTL = 35 * 24 * time.Hour // 월간 재시도 기간 커버
)

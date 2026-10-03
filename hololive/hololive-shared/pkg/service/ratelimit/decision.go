// Package ratelimit는 backend 중립 분산 rate-limit 판정 계약을 소유한다.
// Valkey 슬라이딩 윈도우 구현은 하위 패키지 ratelimit/valkey가 소유하므로,
// 판정 결과만 소비하는 호출자는 backend 의존성을 끌어오지 않는다.
package ratelimit

import "time"

// Decision: 분산 슬라이딩 윈도우 판정 결과.
type Decision struct {
	Allowed    bool
	Current    int
	Remaining  int
	Limit      int
	Window     time.Duration
	RetryAfter time.Duration
}

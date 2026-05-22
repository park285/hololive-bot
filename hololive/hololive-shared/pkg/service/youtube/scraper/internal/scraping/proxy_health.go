package scraping

import "sync"

// ProxyFallbackPolicy: proxy 활성 시 연속 transport 실패가 임계치를 넘으면
// 자동으로 direct 모드로 전환하기 위한 정책. Enabled=false이면 비활성.
type ProxyFallbackPolicy struct {
	Enabled                bool
	MaxConsecutiveFailures int
}

// proxyHealthTracker: proxy client 사용 중의 success/failure 카운트를 추적하여
// 임계치 도달 시 1회 fallback 신호를 반환한다. mu로 동시 호출 안전 보장.
type proxyHealthTracker struct {
	mu                  sync.Mutex
	policy              ProxyFallbackPolicy
	consecutiveFailures int
	triggered           bool
}

func newProxyHealthTracker(policy ProxyFallbackPolicy) *proxyHealthTracker {
	if policy.MaxConsecutiveFailures <= 0 {
		policy.MaxConsecutiveFailures = 5
	}
	return &proxyHealthTracker{policy: policy}
}

// RecordTransportFailure: transport-level 실패를 1회 기록. 임계치 도달 시 true 반환(1회만).
// Enabled=false이면 항상 false.
func (t *proxyHealthTracker) RecordTransportFailure() bool {
	if t == nil || !t.policy.Enabled {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.triggered {
		return false
	}
	t.consecutiveFailures++
	if t.consecutiveFailures >= t.policy.MaxConsecutiveFailures {
		t.triggered = true
		return true
	}
	return false
}

func (t *proxyHealthTracker) RecordSuccess() {
	if t == nil || !t.policy.Enabled {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.consecutiveFailures = 0
	t.triggered = false
}

// Arm: 새로운 관찰 window 시작 — 외부에서 proxy를 재활성화할 때 호출.
func (t *proxyHealthTracker) Arm() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.consecutiveFailures = 0
	t.triggered = false
}

package util

import (
	"log/slog"
	"sync"
	"time"
)

// CircuitState: 서킷 브레이커의 상태 (닫힘, 열림, 반열림)
type CircuitState string

// CircuitState 상수 목록.
const (
	// CircuitStateClosed: 정상 작동 상태 (요청 허용)
	CircuitStateClosed CircuitState = "CLOSED"
	// CircuitStateOpen: 에러 임계치 초과로 인한 차단 상태 (요청 거부)
	CircuitStateOpen CircuitState = "OPEN"
	// CircuitStateHalfOpen: 복구 시도 중인 상태 (제한적 요청 허용)
	CircuitStateHalfOpen CircuitState = "HALF_OPEN"
)

func (s CircuitState) String() string {
	return string(s)
}

// HealthCheckFunction: 외부 서비스의 상태를 점검하는 사용자 정의 함수 타입
type HealthCheckFunction func() bool

// CircuitBreaker: 장애 전파 방지를 위한 서킷 브레이커 패턴 구현체
// 실패 횟수를 모니터링하고 임계치 초과 시 요청을 일시 차단한다.
type CircuitBreaker struct {
	state               CircuitState
	failureCount        int
	failureThreshold    int
	resetTimeout        time.Duration
	nextRetryTime       time.Time
	nextHealthCheckTime time.Time
	healthCheckInterval time.Duration
	isHealthChecking    bool
	healthCheckFn       HealthCheckFunction
	logger              *slog.Logger
	mu                  sync.RWMutex
}

// NewCircuitBreaker: 새로운 서킷 브레이커 인스턴스를 생성합니다.
func NewCircuitBreaker(
	failureThreshold int,
	resetTimeout time.Duration,
	healthCheckInterval time.Duration,
	healthCheckFn HealthCheckFunction,
	logger *slog.Logger,
) *CircuitBreaker {
	return &CircuitBreaker{
		state:               CircuitStateClosed,
		failureCount:        0,
		failureThreshold:    failureThreshold,
		resetTimeout:        resetTimeout,
		healthCheckInterval: healthCheckInterval,
		healthCheckFn:       healthCheckFn,
		logger:              logger,
	}
}

// GetState: 현재 서킷 브레이커의 상태를 반환합니다.
// 상태 조회 시 복구 시간이나 헬스 체크 조건을 확인하여 상태 전이를 트리거할 수도 있다.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitStateOpen {
		now := time.Now()

		if cb.healthCheckFn != nil && now.After(cb.nextHealthCheckTime) && !cb.isHealthChecking {
			go cb.tryHealthCheck()
		} else if cb.healthCheckFn == nil && now.After(cb.nextRetryTime) {
			cb.transitionTo(CircuitStateHalfOpen)
		}
	}

	return cb.state
}

// CanExecute: 현재 요청 실행이 가능한지(서킷이 열려있지 않은지) 확인합니다.
func (cb *CircuitBreaker) CanExecute() bool {
	state := cb.GetState()
	return state != CircuitStateOpen
}

// RecordSuccess: 요청 성공을 기록합니다.
// Half-Open 상태였다면 Closed 상태로 전환하여 서킷을 복구한다.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Info("Circuit Breaker: Service recovered, transitioning to CLOSED")
		cb.transitionTo(CircuitStateClosed)
		cb.failureCount = 0
	} else if cb.state == CircuitStateClosed && cb.failureCount > 0 {
		cb.logger.Debug("Circuit Breaker: Resetting failure count",
			slog.Int("was", cb.failureCount),
		)
		cb.failureCount = 0
	}
}

// RecordFailure: 요청 실패를 기록합니다.
// 실패 횟수가 임계치를 초과하면 서킷을 Open 상태로 전환한다.
func (cb *CircuitBreaker) RecordFailure(customTimeout time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++

	timeout := cb.resetTimeout
	if customTimeout > 0 {
		timeout = customTimeout
	}

	cb.logger.Warn("Circuit Breaker: Failure recorded",
		slog.Int("count", cb.failureCount),
		slog.Int("threshold", cb.failureThreshold),
		slog.Duration("timeout", timeout),
	)

	if cb.state == CircuitStateHalfOpen {
		cb.logger.Error("Circuit Breaker: Recovery failed, reopening circuit")
		cb.transitionTo(CircuitStateOpen)
		cb.nextRetryTime = time.Now().Add(timeout)

		if cb.healthCheckFn != nil {
			cb.nextHealthCheckTime = time.Now().Add(cb.healthCheckInterval)
		}
	} else if cb.failureCount >= cb.failureThreshold {
		cb.logger.Error("Circuit Breaker: Threshold reached, OPENING circuit",
			slog.Int("threshold", cb.failureThreshold),
		)
		cb.transitionTo(CircuitStateOpen)
		cb.nextRetryTime = time.Now().Add(timeout)

		if cb.healthCheckFn != nil {
			cb.nextHealthCheckTime = time.Now().Add(cb.healthCheckInterval)
		}
	}
}

func (cb *CircuitBreaker) tryHealthCheck() {
	cb.mu.Lock()
	if cb.healthCheckFn == nil || cb.isHealthChecking {
		cb.mu.Unlock()
		return
	}
	cb.isHealthChecking = true
	cb.mu.Unlock()

	cb.logger.Info("Circuit Breaker: Running health check...")

	isHealthy := cb.healthCheckFn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.isHealthChecking = false

	if isHealthy {
		cb.logger.Info("Circuit Breaker: Health check PASSED → transitioning to HALF_OPEN")
		cb.transitionTo(CircuitStateHalfOpen)
	} else {
		cb.logger.Warn("Circuit Breaker: Health check FAILED → delaying next check")
		cb.nextHealthCheckTime = time.Now().Add(cb.healthCheckInterval)
	}
}

func (cb *CircuitBreaker) transitionTo(newState CircuitState) {
	oldState := cb.state
	cb.state = newState

	nextRetry := "n/a"
	if newState == CircuitStateOpen {
		nextRetry = cb.nextRetryTime.Format(time.RFC3339)
	}

	cb.logger.Info("Circuit Breaker: State transition",
		slog.String("from", oldState.String()),
		slog.String("to", newState.String()),
		slog.Int("failure_count", cb.failureCount),
		slog.String("next_retry", nextRetry),
	)
}

// Reset: 서킷 브레이커 상태를 강제로 초기화(Closed, 실패 횟수 0)합니다.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.logger.Info("Circuit Breaker: Manual reset")
	cb.state = CircuitStateClosed
	cb.failureCount = 0
	cb.nextRetryTime = time.Time{}
}

// GetStatus: 모니터링을 위해 현재 서킷 브레이커의 상세 상태 정보를 반환합니다.
func (cb *CircuitBreaker) GetStatus() CircuitBreakerStatus {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	status := CircuitBreakerStatus{
		State:        cb.state, // \uc9c1\uc811 \uc811\uadfc (\uc774\ubbf8 lock \ud68d\ub4dd \uc0c1\ud0dc)
		FailureCount: cb.failureCount,
	}

	if cb.state == CircuitStateOpen {
		status.NextRetryTime = &cb.nextRetryTime
	}

	return status
}

// CircuitBreakerStatus: 서킷 브레이커의 상세 상태 정보 (스냅샷)
type CircuitBreakerStatus struct {
	State         CircuitState
	FailureCount  int
	NextRetryTime *time.Time
}

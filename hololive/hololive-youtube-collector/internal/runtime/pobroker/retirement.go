package pobroker

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ExitReason은 generation 퇴역(곧 프로세스 종료) 원인의 고정 어휘입니다. 종료
// 로그에는 이 값만 기록하고 요청 본문·token·worker 출력은 싣지 않습니다.
type ExitReason string

const (
	// ExitSessionClosed는 client의 DELETE /v1/session 반납입니다.
	ExitSessionClosed ExitReason = "session_closed"
	// ExitLeaseExpired는 lease 타이머나 요청 시점 검사가 만료를 확인한 경우입니다.
	ExitLeaseExpired ExitReason = "lease_expired"
	// ExitWorkerFailed는 worker의 잘못된 응답·IO 실패입니다.
	ExitWorkerFailed ExitReason = "worker_failed"
	// ExitWorkerTimeout은 operationLimit 초과입니다.
	ExitWorkerTimeout ExitReason = "worker_timeout"
	// ExitRequestAborted는 worker IO 중 요청 연결이 끊긴 경우입니다.
	ExitRequestAborted ExitReason = "request_aborted"
	// ExitResponseFailed는 worker 결과를 응답으로 인코딩·전송하지 못한 경우입니다.
	ExitResponseFailed ExitReason = "response_failed"
	// ExitWorkerExited는 기동을 마친 worker가 worker IO 요청이 없는 동안 스스로 종료한 경우입니다.
	ExitWorkerExited ExitReason = "worker_exited"
	// ExitStartupFailed는 worker 기동·loaded 확인 실패입니다.
	ExitStartupFailed ExitReason = "startup_failed"
	// ExitListenerFailed는 listener 준비 또는 Serve 자체의 실패입니다.
	ExitListenerFailed ExitReason = "listener_failed"
	// ExitSignal은 퇴역이 시작되기 전에 받은 종료 signal입니다. 퇴역 중 받은 signal은 먼저 표시된 원인을 남깁니다.
	ExitSignal ExitReason = "signal"
)

// ExitReason은 처음 기록된 퇴역 원인을 돌려줍니다. Serve가 반환한 뒤에는
// 항상 비어 있지 않습니다.
func (b *Broker) ExitReason() ExitReason {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.exitReason
}

// retire는 정리에 실패해도 세대와 listener의 수명을 끝낸다. 오류는 Serve에서만 보고한다.
func (b *Broker) retire(reason ExitReason) {
	b.retireOnce.Do(func() {
		b.mu.Lock()

		b.markRetiringLocked(reason)

		if b.leaseTimer != nil {
			b.leaseTimer.Stop()
		}

		w := b.worker
		b.mu.Unlock()

		err := w.stop()

		if b.server != nil {
			err = errors.Join(err, b.server.Close())
		}

		b.retireErr = err
	})
}

// markRetiringLocked는 generation을 퇴역 중으로 표시합니다. 퇴역 원인은 처음
// 표시한 호출의 것만 남습니다. 호출자는 b.mu를 소유해야 합니다.
func (b *Broker) markRetiringLocked(reason ExitReason) {
	b.retiring = true
	if b.exitReason == "" {
		b.exitReason = reason
	}
}

// lease는 요청이 끊겨도 비신뢰 VM을 무기한 유지하지 않도록 broker 수명에 묶습니다.
// 호출자는 b.mu를 소유해야 합니다.
func (b *Broker) setLeaseLocked(state State, deadline time.Time) {
	b.state, b.expires = state, deadline
	if b.leaseTimer == nil {
		b.leaseTimer = time.AfterFunc(time.Until(deadline), b.expireLease)
	} else {
		b.leaseTimer.Reset(time.Until(deadline))
	}
}

func (b *Broker) expireLease() {
	b.mu.Lock()

	if b.retiring || b.expires.IsZero() || time.Now().Before(b.expires) {
		b.mu.Unlock()

		return
	}

	b.markRetiringLocked(ExitLeaseExpired)
	b.mu.Unlock()
	b.retire(ExitLeaseExpired)
}

func (b *Broker) workerFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		failure(w, http.StatusGatewayTimeout, "worker_timeout")
		b.retireAfterResponse(w, ExitRequestAborted)
	case errors.Is(err, errBeforeDispatch) || errors.Is(err, context.DeadlineExceeded):
		failure(w, http.StatusGatewayTimeout, "worker_timeout")
		b.retireAfterResponse(w, ExitWorkerTimeout)
	default:
		failure(w, http.StatusServiceUnavailable, "worker_failed")
		b.retireAfterResponse(w, ExitWorkerFailed)
	}
}

func (b *Broker) retireAfterResponse(w http.ResponseWriter, reason ExitReason) {
	b.mu.Lock()

	b.markRetiringLocked(reason)
	b.mu.Unlock()

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go b.retire(reason)
}

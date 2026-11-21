package pobroker

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type ExitReason string

const (
	ExitSessionClosed  ExitReason = "session_closed"
	ExitLeaseExpired   ExitReason = "lease_expired"
	ExitWorkerFailed   ExitReason = "worker_failed"
	ExitWorkerTimeout  ExitReason = "worker_timeout"
	ExitRequestAborted ExitReason = "request_aborted"
	ExitResponseFailed ExitReason = "response_failed"
	ExitWorkerExited   ExitReason = "worker_exited"
	ExitStartupFailed  ExitReason = "startup_failed"
	ExitListenerFailed ExitReason = "listener_failed"
	ExitSignal         ExitReason = "signal"
)

func (b *Broker) ExitReason() ExitReason {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.exitReason
}

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

func (b *Broker) markRetiringLocked(reason ExitReason) {
	b.retiring = true
	if b.exitReason == "" {
		b.exitReason = reason
	}
}

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

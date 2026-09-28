package pobroker

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestPendingLeaseExpiresWithoutAnotherRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("revision", "unused", "unused")
		broker.mu.Lock()
		broker.setLeaseLocked(AwaitingIntegrity, time.Now().Add(30*time.Second))
		broker.mu.Unlock()

		status, _ := call(t, broker, http.MethodGet, "/health", "")
		if status != http.StatusOK {
			t.Fatal("pending lease was not initially available")
		}

		time.Sleep(30 * time.Second)
		synctest.Wait()

		status, body := call(t, broker, http.MethodGet, "/health", "")
		if status != http.StatusServiceUnavailable || errorCode(body) != "worker_failed" {
			t.Fatalf("abandoned generation remained available: %d %v", status, body)
		}

		if reason := broker.ExitReason(); reason != ExitLeaseExpired {
			t.Fatalf("lease expiry recorded exit reason %q", reason)
		}
	})
}

func TestActivationLeaseReplacesPendingDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("revision", "unused", "unused")
		broker.mu.Lock()
		broker.setLeaseLocked(AwaitingIntegrity, time.Now().Add(30*time.Second))
		broker.mu.Unlock()
		time.Sleep(20 * time.Second)
		broker.mu.Lock()
		broker.setLeaseLocked(Ready, time.Now().Add(time.Minute))
		broker.mu.Unlock()
		time.Sleep(10 * time.Second)
		synctest.Wait()

		status, body := call(t, broker, http.MethodGet, "/health", "")
		if status != http.StatusOK || body["state"] != string(Ready) {
			t.Fatal("old pending deadline retired active generation")
		}

		time.Sleep(50 * time.Second)
		synctest.Wait()

		status, _ = call(t, broker, http.MethodGet, "/health", "")
		if status != http.StatusServiceUnavailable {
			t.Fatal("active generation outlived its monotonic lease")
		}
	})
}

func TestCanceledUnadmittedRequestDoesNotRetireGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("revision", "unused", "unused")
		broker.serial <- struct{}{}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: broker.Generation()})
		if err != nil {
			t.Fatal(err)
		}

		request := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/v1/session", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")

		response := httptest.NewRecorder()
		broker.handle(response, request)
		<-broker.serial
		synctest.Wait()

		if response.Code != http.StatusGatewayTimeout {
			t.Fatal("canceled request was admitted")
		}

		status, state := call(t, broker, http.MethodGet, "/health", "")
		if status != http.StatusOK || state["state"] != string(Idle) {
			t.Fatal("unadmitted cancellation retired another operation's generation")
		}
	})
}

func TestCanceledAvailableSlotPreservesReadyGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("revision", "unused", "unused")

		broker.state = Ready

		body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: broker.Generation()})
		if err != nil {
			t.Fatal(err)
		}

		for range 32 {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			request := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/v1/session", strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")

			response := httptest.NewRecorder()
			broker.handle(response, request)
			synctest.Wait()

			if response.Code != http.StatusGatewayTimeout {
				t.Fatal("canceled request was admitted")
			}

			status, state := call(t, broker, http.MethodGet, "/health", "")
			if status != http.StatusOK || state["state"] != string(Ready) {
				t.Fatal("pre-operation cancellation retired the ready generation")
			}
		}
	})
}

func TestSessionCloseRecordsExitReason(t *testing.T) {
	broker := New("revision", "unused", "unused")

	body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: broker.Generation()})
	if err != nil {
		t.Fatal(err)
	}

	status, result := call(t, broker, http.MethodDelete, "/v1/session", string(body))
	if status != http.StatusOK || result["closed"] != true {
		t.Fatalf("session was not closed: %d %v", status, result)
	}

	if reason := broker.ExitReason(); reason != ExitSessionClosed {
		t.Fatalf("session close recorded exit reason %q", reason)
	}
}

// 응답 뒤 비동기 retire, worker 종료 감시, lease 타이머가 동시에 퇴역을 요청해도
// 먼저 퇴역을 표시한 원인만 남아야 합니다.
func TestFirstRetirementReasonWinsUnderConcurrentRetire(t *testing.T) {
	broker := New("revision", "unused", "unused")
	// 만료된 lease를 타이머 없이 둡니다. setLeaseLocked는 지난 deadline이면 곧바로 발화하는 타이머를 걸어,
	// 첫 퇴역 표시보다 lease_expired가 먼저 기록될 수 있습니다. 타이머 경합은 아래 expireLease 호출이 맡습니다.
	// 아직 다른 goroutine이 없으므로 잠금 없이 씁니다.
	broker.state, broker.expires = Ready, time.Now().Add(-time.Second)

	broker.retireAfterResponse(httptest.NewRecorder(), ExitSessionClosed)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for _, reason := range []ExitReason{ExitWorkerExited, ExitWorkerFailed, ExitListenerFailed, ExitStartupFailed} {
		wg.Go(func() {
			<-start
			broker.retire(reason)
		})
		wg.Go(func() {
			<-start
			broker.retireAfterResponse(httptest.NewRecorder(), reason)
		})
		wg.Go(func() {
			<-start
			broker.expireLease()
		})
	}

	close(start)
	wg.Wait()
	broker.retire(ExitSignal)

	if reason := broker.ExitReason(); reason != ExitSessionClosed {
		t.Fatalf("later retirement replaced the first exit reason: %q", reason)
	}
}

func TestRacingRetirementsRecordExactlyOneReason(t *testing.T) {
	broker := New("revision", "unused", "unused")
	contenders := []ExitReason{ExitWorkerExited, ExitWorkerFailed, ExitLeaseExpired, ExitSessionClosed}

	var wg sync.WaitGroup

	start := make(chan struct{})

	for _, reason := range contenders {
		wg.Go(func() {
			<-start
			broker.retire(reason)
		})
		wg.Go(func() {
			<-start
			broker.retireAfterResponse(httptest.NewRecorder(), reason)
		})
	}

	close(start)
	wg.Wait()

	first := broker.ExitReason()
	if !slices.Contains(contenders, first) {
		t.Fatalf("racing retirements recorded %q", first)
	}

	broker.retire(ExitSignal)

	if reason := broker.ExitReason(); reason != first {
		t.Fatalf("exit reason changed from %q to %q after retirement", first, reason)
	}
}

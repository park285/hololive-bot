package pobroker

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
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

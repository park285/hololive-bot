package pobroker

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// silentNode never answers, so any progress past it must come from retirement.
const silentNode = "#!/bin/sh\nsleep 30\n"

// fakeNode writes a shell stand-in for the Node executable into a private
// test directory and returns its path.
func fakeNode(t *testing.T, name, script string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	// startWorker execs this file directly as the VM binary.
	//nolint:gosec // G306: the fixture needs owner execute and lives in this test's private t.TempDir.
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

// errorCode returns the broker error code, or "" when body is not an error.
func errorCode(body map[string]any) string {
	envelope, ok := body["error"].(map[string]any)
	if !ok {
		return ""
	}

	code, ok := envelope["code"].(string)
	if !ok {
		return ""
	}

	return code
}

func call(t *testing.T, b *Broker, method, path, body string) (int, map[string]any) {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	b.handle(w, r)

	var result map[string]any

	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	return w.Code, result
}

func TestRestartChangesGenerationAndRejectsStaleOperations(t *testing.T) {
	first := New("revision", "unused", "unused")
	second := New("revision", "unused", "unused")

	if first.Generation() == second.Generation() {
		t.Fatal("restart reused generation")
	}

	status, health := call(t, second, http.MethodGet, "/health", "")
	if status != http.StatusOK || health["generation"] != second.Generation() || health["state"] != string(Idle) {
		t.Fatalf("unexpected fresh health: %d %v", status, health)
	}

	body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: first.Generation()})
	if err != nil {
		t.Fatal(err)
	}

	status, result := call(t, second, http.MethodDelete, "/v1/session", string(body))
	if status != http.StatusConflict || errorCode(result) != "generation_mismatch" {
		t.Fatalf("stale reset accepted: %d %v", status, result)
	}

	status, health = call(t, second, http.MethodGet, "/health", "")
	if status != http.StatusOK || health["state"] != string(Idle) {
		t.Fatal("stale request changed generation state")
	}
}

func TestExpiredMintRetiresWorkerWithoutIssuing(t *testing.T) {
	// The executable never supplies a result. Expiry must be decided before
	// attempting IO, and retiring must kill the entire worker process group.
	path := fakeNode(t, "silent-node", silentNode)

	process, err := startWorker(t.Context(), path, "unused")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if stopErr := process.stop(); stopErr != nil {
			t.Error(stopErr)
		}
	})

	b := New("revision", path, "unused")

	b.worker, b.state, b.used, b.expires = process, Ready, true, time.Now().Add(-time.Second)

	body, err := json.Marshal(mintRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), VideoID: "video"})
	if err != nil {
		t.Fatal(err)
	}

	status, result := call(t, b, http.MethodPost, "/v1/mint", string(body))
	if status != http.StatusConflict || errorCode(result) != "expired" {
		t.Fatalf("expired token path not rejected: %d %v", status, result)
	}

	select {
	case <-process.done:
	case <-time.After(2 * time.Second):
		t.Fatal("expired VM survived retirement")
	}
}

func phaseBroker(t *testing.T) (*Broker, string, string, string) {
	t.Helper()

	script := "#!/bin/sh\nIFS= read -r request\nprintf '%s\\n' '{\"type\":\"prepared\",\"prepared\":true}'\n" +
		"IFS= read -r request\nprintf '%s\\n' '{\"type\":\"snapshot\",\"snapshot\":\"snapshot-value\"}'\n" +
		"IFS= read -r request\nprintf '%s\\n' '{\"type\":\"ready\",\"ready\":true}'\nsleep 30\n"

	b := New("revision", fakeNode(t, "phase-node", script), "unused")

	prepare, err := json.Marshal(sessionRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), UserAgent: "UA"})
	if err != nil {
		t.Fatal(err)
	}

	challenge, err := json.Marshal(challengeRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), Program: "program", GlobalName: "name", Interpreter: "code"})
	if err != nil {
		t.Fatal(err)
	}

	activation, err := json.Marshal(activateRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), IntegrityToken: "token", ValidForMS: 60000})
	if err != nil {
		t.Fatal(err)
	}

	return b, string(prepare), string(challenge), string(activation)
}

func TestPrepareChallengePreservesPendingLease(t *testing.T) {
	b, prepare, challenge, activation := phaseBroker(t)
	defer b.retire()

	status, result := call(t, b, http.MethodPost, "/v1/session", prepare)
	if status != http.StatusOK || result["prepared"] != true || result["generation"] != b.Generation() {
		t.Fatalf("prepare did not initialize worker: %d %v", status, result)
	}

	b.mu.RLock()

	deadline := b.expires
	b.mu.RUnlock()

	status, result = call(t, b, http.MethodGet, "/health", "")
	if status != http.StatusOK || result["state"] != string(AwaitingChallenge) || deadline.IsZero() {
		t.Fatalf("prepared worker lacked pending lease: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/challenge", challenge)
	if status != http.StatusOK || result["snapshot"] != "snapshot-value" || result["generation"] != b.Generation() {
		t.Fatalf("challenge did not return snapshot: %d %v", status, result)
	}

	b.mu.RLock()

	stillPending := deadline.Equal(b.expires)
	b.mu.RUnlock()

	status, result = call(t, b, http.MethodGet, "/health", "")
	if status != http.StatusOK || result["state"] != string(AwaitingIntegrity) || !stillPending {
		t.Fatalf("challenge reset prepare lease: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/activate", activation)
	if status != http.StatusOK || result["ready"] != true {
		t.Fatalf("activation after challenge failed: %d %v", status, result)
	}
}

func TestChallengeWrongPhaseDoesNotConsumeWorker(t *testing.T) {
	b, prepare, challenge, activation := phaseBroker(t)
	defer b.retire()

	status, result := call(t, b, http.MethodPost, "/v1/challenge", challenge)
	if status != http.StatusConflict || errorCode(result) != "invalid_state" {
		t.Fatalf("challenge before prepare accepted: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/session", prepare)
	if status != http.StatusOK || result["prepared"] != true {
		t.Fatalf("prepare after rejected challenge failed: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/activate", activation)
	if status != http.StatusConflict || errorCode(result) != "invalid_state" {
		t.Fatalf("activate before challenge accepted: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/challenge", challenge)
	if status != http.StatusOK || result["snapshot"] != "snapshot-value" {
		t.Fatalf("wrong phase consumed prepared worker: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/challenge", challenge)
	if status != http.StatusConflict || errorCode(result) != "invalid_state" {
		t.Fatalf("replayed challenge accepted: %d %v", status, result)
	}
}

func TestCanceledChallengeBeforeDispatchPreservesPreparedWorker(t *testing.T) {
	b, prepare, challenge, _ := phaseBroker(t)
	defer b.retire()

	status, result := call(t, b, http.MethodPost, "/v1/session", prepare)
	if status != http.StatusOK || result["prepared"] != true {
		t.Fatalf("worker did not prepare: %d %v", status, result)
	}

	b.serial <- struct{}{}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/challenge", strings.NewReader(challenge))
	r.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	b.handle(w, r)
	<-b.serial

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("canceled challenge was admitted: %d", w.Code)
	}

	status, result = call(t, b, http.MethodGet, "/health", "")
	if status != http.StatusOK || result["state"] != string(AwaitingChallenge) {
		t.Fatalf("pre-dispatch cancellation retired prepared worker: %d %v", status, result)
	}

	status, result = call(t, b, http.MethodPost, "/v1/challenge", challenge)
	if status != http.StatusOK || result["snapshot"] != "snapshot-value" {
		t.Fatalf("cancellation consumed challenge: %d %v", status, result)
	}
}

func TestMalformedWorkerFrameFailsAndRetires(t *testing.T) {
	script := "#!/bin/sh\nIFS= read -r request\nprintf '%s\\n' '{\"type\":\"prepared\",\"prepared\":true}'\n" +
		"IFS= read -r request\nprintf '%s\\n' '{\"type\":\"snapshot\",\"snapshot\":\"x\",\"extra\":true}'\nsleep 30\n"

	b := New("revision", fakeNode(t, "invalid-node", script), "unused")

	prepare, err := json.Marshal(sessionRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), UserAgent: "UA"})
	if err != nil {
		t.Fatal(err)
	}

	status, result := call(t, b, http.MethodPost, "/v1/session", string(prepare))
	if status != http.StatusOK || result["prepared"] != true {
		t.Fatalf("worker did not prepare: %d %v", status, result)
	}

	challenge, err := json.Marshal(challengeRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), Program: "program", GlobalName: "name", Interpreter: "code"})
	if err != nil {
		t.Fatal(err)
	}

	status, result = call(t, b, http.MethodPost, "/v1/challenge", string(challenge))
	if status != http.StatusServiceUnavailable || errorCode(result) != "worker_failed" {
		t.Fatalf("malformed worker result accepted: %d %v", status, result)
	}

	b.mu.RLock()

	process := b.worker
	b.mu.RUnlock()

	if process == nil {
		t.Fatal("worker never spawned")
	}

	select {
	case <-process.done:
	case <-time.After(2 * time.Second):
		t.Fatal("malformed worker survived retirement")
	}
}

func TestActiveCancellationRetiresWorker(t *testing.T) {
	b := New("revision", fakeNode(t, "blocked-node", silentNode), "unused")

	body, err := json.Marshal(sessionRequest{ProtocolVersion: protocolVersion, Generation: b.Generation(), UserAgent: "UA"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

	defer cancel()

	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/session", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	b.handle(w, r)

	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	if w.Code != http.StatusGatewayTimeout || response.Error.Code != "worker_timeout" {
		t.Fatalf("cancellation did not fail closed: %d %s", w.Code, response.Error.Code)
	}

	b.mu.RLock()

	process := b.worker
	b.mu.RUnlock()

	if process == nil {
		t.Fatal("worker was not started")
	}

	select {
	case <-process.done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled VM survived retirement")
	}
}

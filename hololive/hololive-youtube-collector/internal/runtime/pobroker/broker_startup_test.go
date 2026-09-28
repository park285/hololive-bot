package pobroker

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeDoesNotReportHealthyBeforeWorkerLoads(t *testing.T) {
	script := "#!/bin/sh\nIFS= read -r release\nprintf '%s\\n' '{\"type\":\"loaded\"}'\nsleep 30\n"
	broker := New("revision", fakeNode(t, "loading-node", script), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() { done <- broker.Serve(listener) }()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("broker did not finish retirement")
		}
	})

	process := waitForStartedWorker(t, broker)

	var dialer net.Dialer

	conn, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+listener.Addr().String()+"/health", http.NoBody)
	require.NoError(t, err)
	require.NoError(t, request.Write(conn))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(100*time.Millisecond)))

	var firstByte [1]byte

	_, err = conn.Read(firstByte[:])
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "health became available before worker loading")

	_, err = process.stdin.Write([]byte("load\n"))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	require.NoError(t, err)

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("loaded worker health = %d, want 200", response.StatusCode)
	}
}

func TestInvalidStartupFrameClosesListenerAndReapsWorker(t *testing.T) {
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"loaded\",\"extra\":true}'\nsleep 30\n"
	broker := New("revision", fakeNode(t, "invalid-startup-node", script), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if err := broker.Serve(listener); err == nil {
		t.Fatal("invalid startup frame was accepted")
	}

	if reason := broker.ExitReason(); reason != ExitStartupFailed {
		t.Fatalf("failed startup recorded exit reason %q", reason)
	}

	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("failed startup left listener open: %v", err)
	}

	select {
	case <-broker.worker.done:
	default:
		t.Fatal("failed startup left the worker running")
	}
}

func TestServeStartupDeadlineClosesListenerAndReapsWorker(t *testing.T) {
	broker := New("revision", fakeNode(t, "silent-startup-node", "#!/bin/sh\nsleep 60\n"), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() {
		done <- broker.Serve(listener)

		close(done)
	}()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("silent startup did not retire")
		}
	})

	process := waitForStartedWorker(t, broker)

	// 실제 30초 기동 deadline을 사용한다. 테스트용 가변 상한이나 runtime 분기는 추가하지 않는다.
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(40 * time.Second):
		t.Fatal("silent worker exceeded the startup bound")
	}

	require.ErrorIs(t, listener.Close(), net.ErrClosed)

	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out startup left the worker running")
	}
}

// 정상 응답 뒤에는 서버가 연결을 닫지 않고, 퇴역하면 유휴 keep-alive 연결도
// IdleTimeout을 기다리지 않고 즉시 닫혀 Serve가 반환해야 합니다.
func TestServeKeepsConnectionsUntilRetirementClosesThem(t *testing.T) {
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"loaded\"}'\nsleep 30\n"
	broker := New("revision", fakeNode(t, "keepalive-node", script), "unused")
	socket := filepath.Join(t.TempDir(), "worker.sock")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "unix", socket)
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() {
		done <- broker.Serve(listener)

		close(done)
	}()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("broker did not finish retirement")
		}
	})

	active := dialBroker(t, socket)
	idle := dialBroker(t, socket)

	for range 2 {
		require.Equal(t, http.StatusOK, roundTrip(t, active, http.MethodGet, "/health", ""))
	}

	require.Equal(t, http.StatusOK, roundTrip(t, idle, http.MethodGet, "/health", ""))

	idleSince := time.Now()
	body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: broker.Generation()})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, roundTrip(t, active, http.MethodDelete, "/v1/session", string(body)))

	// 서버 IdleTimeout(2초)보다 먼저 닫혀야 퇴역이 닫은 것입니다.
	require.NoError(t, idle.conn.SetReadDeadline(idleSince.Add(time.Second)))

	_, err = idle.reader.ReadByte()
	require.ErrorIs(t, err, io.EOF, "retirement left an idle keep-alive connection open")

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("retired broker kept serving")
	}

	require.Equal(t, ExitSessionClosed, broker.ExitReason())
}

type brokerConn struct {
	conn   net.Conn
	reader *bufio.Reader
}

func dialBroker(t *testing.T, socket string) brokerConn {
	t.Helper()

	var dialer net.Dialer

	conn, err := dialer.DialContext(t.Context(), "unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })

	return brokerConn{conn: conn, reader: bufio.NewReader(conn)}
}

// roundTrip은 같은 연결에서 요청 하나를 보내고 응답을 끝까지 읽습니다. 서버가
// 응답 뒤 연결을 닫겠다고 알리면 keep-alive 계약 위반으로 실패합니다.
func roundTrip(t *testing.T, c brokerConn, method, path, body string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, "http://po-broker"+path, strings.NewReader(body))
	require.NoError(t, err)

	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	// 첫 요청은 worker 기동이 끝날 때까지 accept 대기열에 머뭅니다.
	require.NoError(t, c.conn.SetDeadline(time.Now().Add(10*time.Second)))
	require.NoError(t, request.Write(c.conn))

	response, err := http.ReadResponse(c.reader, request)
	require.NoError(t, err)

	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.False(t, response.Close, "broker closed the connection after a normal response")

	return response.StatusCode
}

func waitForStartedWorker(t *testing.T, broker *Broker) *worker {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		broker.mu.RLock()

		process := broker.worker
		broker.mu.RUnlock()

		if process != nil {
			return process
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("worker did not start")

	return nil
}

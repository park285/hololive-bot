package pobroker

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
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
		broker.retire()

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
		broker.retire()

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

package pobroker

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeDoesNotReportHealthyBeforeWorkerLoads(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "load")
	require.NoError(t, syscall.Mkfifo(gate, 0o600))

	script := "#!/bin/sh\nfor gate do :; done\nIFS= read -r release < \"$gate\"\nprintf '%s\\n' '{\"type\":\"loaded\"}'\nIFS= read -r stop\n"
	broker := New("revision", fakeNode(t, "loading-node", script), gate)

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() { done <- broker.Serve(listener, 30*time.Second) }()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case serveErr := <-done:
			if serveErr != nil {
				t.Errorf("Serve() after retirement error = %v, want nil", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("broker did not finish retirement")
		}
	})

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })

	release, err := root.OpenFile("load", os.O_WRONLY, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, release.Close()) })

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

	_, err = release.WriteString("load\n")
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
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"loaded\",\"extra\":true}'\nIFS= read -r stop\n"
	broker := New("revision", fakeNode(t, "invalid-startup-node", script), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	if err := broker.Serve(listener, 30*time.Second); err == nil {
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

func TestWorkerStartupDeadlineBeforeDispatch(t *testing.T) {
	broker := New("revision", "unused", "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	err = broker.Serve(listener, 0)
	require.ErrorIs(t, err, errBeforeDispatch)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, ExitStartupFailed, broker.ExitReason())
	require.ErrorIs(t, listener.Close(), net.ErrClosed)
}

func TestSilentLoadedFrameExpiresAtStartupDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		broker := New("revision", "unused", "unused")
		reader, writer := io.Pipe()
		done := make(chan struct{})

		var loadErr error

		defer func() { <-done }()
		defer func() { require.NoError(t, writer.Close()) }()

		process := &worker{out: bufio.NewReader(reader), done: make(chan struct{})}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		go func() {
			defer close(done)

			loadErr = broker.loadWorker(ctx, process)
		}()

		synctest.Sleep(time.Second - time.Nanosecond)

		select {
		case <-done:
			t.Fatalf("worker loading returned before the deadline: %v", loadErr)
		default:
		}

		synctest.Sleep(time.Nanosecond)

		select {
		case <-done:
			require.ErrorIs(t, loadErr, context.DeadlineExceeded)
		default:
			t.Fatal("worker loading did not return at the deadline")
		}
	})
}

func TestServeStartupDeadlineClosesListenerAndReapsWorker(t *testing.T) {
	broker := New("revision", fakeNode(t, "silent-startup-node", "#!/bin/sh\nIFS= read -r stop\n"), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})

	var serveErr error

	go func() {
		defer close(done)

		serveErr = broker.Serve(listener, time.Second)
	}()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("silent startup did not retire")
		}
	})

	select {
	case <-done:
		require.ErrorIs(t, serveErr, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("silent worker exceeded the startup bound")
	}

	require.Equal(t, ExitStartupFailed, broker.ExitReason())
	require.ErrorIs(t, listener.Close(), net.ErrClosed)

	require.NotNil(t, broker.worker)
	requireWorkerReaped(t, broker.worker)
}

func TestDeadlineAfterWorkerStartBeforeRegistration(t *testing.T) {
	broker, process := unregisteredWorker(t)
	ctx, cancel := context.WithTimeout(t.Context(), 0)

	defer cancel()

	err := broker.loadWorker(ctx, process)
	require.ErrorIs(t, err, errWorker)
	require.ErrorIs(t, err, errBeforeDispatch)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	requireWorkerReaped(t, process)
}

func TestCancellationAfterWorkerStartBeforeRegistration(t *testing.T) {
	broker, process := unregisteredWorker(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := broker.loadWorker(ctx, process)
	require.ErrorIs(t, err, errWorker)
	require.NotErrorIs(t, err, errBeforeDispatch)
	require.NotErrorIs(t, err, context.Canceled)
	requireWorkerReaped(t, process)
}

func unregisteredWorker(t *testing.T) (*Broker, *worker) {
	t.Helper()

	broker := New("revision", fakeNode(t, "silent-node", "#!/bin/sh\nIFS= read -r stop\n"), "unused")
	process, err := startWorker(t.Context(), broker.node, broker.script)
	require.NoError(t, err)
	t.Cleanup(func() {
		select {
		case <-process.done:
		default:
			require.NoError(t, process.stop())
		}
	})

	return broker, process
}

func requireWorkerReaped(t *testing.T, process *worker) {
	t.Helper()

	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("startup worker was not reaped")
	}
}

func TestServeKeepsConnectionsUntilRetirementClosesThem(t *testing.T) {
	broker, socket, done := serveKeepAliveBroker(t)

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

func TestSessionCloseResponseHasContentLength(t *testing.T) {
	broker, socket, _ := serveKeepAliveBroker(t)
	c := dialBroker(t, socket)

	require.Equal(t, http.StatusOK, roundTrip(t, c, http.MethodGet, "/health", ""))

	body, err := json.Marshal(envelope{ProtocolVersion: protocolVersion, Generation: broker.Generation()})
	require.NoError(t, err)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, "http://po-broker/v1/session", strings.NewReader(string(body)))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	require.NoError(t, c.conn.SetDeadline(time.Now().Add(10*time.Second)))
	require.NoError(t, request.Write(c.conn))

	response, err := http.ReadResponse(c.reader, request)
	require.NoError(t, err)

	defer response.Body.Close()

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Positive(t, response.ContentLength)
	require.Empty(t, response.TransferEncoding)
}

func TestFlushedResponsesHaveContentLength(t *testing.T) {
	tests := []struct {
		name   string
		write  func(http.ResponseWriter)
		status int
	}{
		{"success", func(w http.ResponseWriter) { success(w, struct{ Closed bool }{true}) }, http.StatusOK},
		{"failure", func(w http.ResponseWriter) { failure(w, http.StatusConflict, "expired") }, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tt.write(w)

				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("Flush() error = %v", err)
				}
			}))
			t.Cleanup(server.Close)

			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, http.NoBody)
			require.NoError(t, err)

			response, err := server.Client().Do(request)
			require.NoError(t, err)

			defer response.Body.Close()

			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, tt.status, response.StatusCode)
			require.Equal(t, int64(len(data)), response.ContentLength)
			require.Positive(t, response.ContentLength)
			require.Empty(t, response.TransferEncoding)
		})
	}
}

func TestServeKeepsIdleConnectionPastClientStallWindow(t *testing.T) {
	broker, socket, _ := serveKeepAliveBroker(t)
	conn := dialBroker(t, socket)
	require.Equal(t, http.StatusOK, roundTrip(t, conn, http.MethodGet, "/health", ""))

	synctest.Test(t, func(t *testing.T) {
		server := httptest.NewTestServer(t, broker.server.Handler)

		server.Config.IdleTimeout = broker.server.IdleTimeout
		server.Config.ReadHeaderTimeout = broker.server.ReadHeaderTimeout
		server.Config.ReadTimeout = broker.server.ReadTimeout

		var closed atomic.Bool

		server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed {
				closed.Store(true)
			}
		}

		client := server.Client()

		var first net.Conn

		for _, wantReused := range []bool{false, true} {
			if wantReused {
				synctest.Sleep(2500 * time.Millisecond)
			}

			var connections []httptrace.GotConnInfo

			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
				GotConn: func(info httptrace.GotConnInfo) { connections = append(connections, info) },
			})
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/health", http.NoBody)
			require.NoError(t, err)

			response, err := client.Do(request)
			require.NoError(t, err)

			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, response.Body.Close())
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Len(t, connections, 1, "health request must succeed without a connection retry")
			require.Equal(t, wantReused, connections[0].Reused, "second health request must reuse the idle connection")

			if wantReused {
				require.Same(t, first, connections[0].Conn, "health requests must use the same connection")
			} else {
				first = connections[0].Conn
			}
		}

		synctest.Wait()
		synctest.Sleep(30*time.Second - time.Nanosecond)

		if closed.Load() {
			t.Fatal("broker closed the connection before 30 seconds idle")
		}

		synctest.Sleep(time.Nanosecond)

		if !closed.Load() {
			t.Fatal("broker kept the connection open after 30 seconds idle")
		}
	})
}

func serveKeepAliveBroker(t *testing.T) (broker *Broker, socket string, done <-chan error) {
	t.Helper()

	broker, socket, listener := newKeepAliveBroker(t)

	return broker, socket, serveBroker(t, broker, listener)
}

func newKeepAliveBroker(t *testing.T) (*Broker, string, net.Listener) {
	t.Helper()

	const script = "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"loaded\"}'\nIFS= read -r release\n"

	broker := New("revision", fakeNode(t, "keepalive-node", script), "unused")
	socket := filepath.Join(t.TempDir(), "worker.sock")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "unix", socket)
	require.NoError(t, err)

	return broker, socket, listener
}

func serveBroker(t *testing.T, broker *Broker, listener net.Listener) <-chan error {
	t.Helper()

	served := make(chan error, 1)

	go func() {
		served <- broker.Serve(listener, 30*time.Second)

		close(served)
	}()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case serveErr := <-served:
			if serveErr != nil {
				t.Errorf("Serve() after retirement error = %v, want nil", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("broker did not finish retirement")
		}
	})

	return served
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

func roundTrip(t *testing.T, c brokerConn, method, path, body string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, "http://po-broker"+path, strings.NewReader(body))
	require.NoError(t, err)

	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

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

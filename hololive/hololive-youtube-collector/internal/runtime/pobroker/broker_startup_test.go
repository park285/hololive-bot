package pobroker

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServeDoesNotReportHealthyBeforeWorkerLoads(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "load")
	require.NoError(t, syscall.Mkfifo(gate, 0o600))

	script := "#!/bin/sh\nfor gate do :; done\nIFS= read -r release < \"$gate\"\nprintf '%s\\n' '{\"type\":\"loaded\"}'\nsleep 30\n"
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
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"loaded\",\"extra\":true}'\nsleep 30\n"
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

func TestServeStartupDeadlineClosesListenerAndReapsWorker(t *testing.T) {
	broker := New("revision", fakeNode(t, "silent-startup-node", "#!/bin/sh\nsleep 60\n"), "unused")

	var config net.ListenConfig

	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)

	go func() {
		done <- broker.Serve(listener, time.Second)

		close(done)
	}()

	t.Cleanup(func() {
		broker.retire(ExitSignal)

		select {
		case serveErr := <-done:
			if serveErr != nil && !errors.Is(serveErr, context.DeadlineExceeded) {
				t.Errorf("Serve() after silent startup error = %v, want startup deadline", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("silent startup did not retire")
		}
	})

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("silent worker exceeded the startup bound")
	}

	require.ErrorIs(t, listener.Close(), net.ErrClosed)

	select {
	case <-broker.worker.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed-out startup left the worker running")
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
	broker, socket, listener := newKeepAliveBroker(t)
	idleDeadline := make(chan time.Duration, 1)
	serveBroker(t, broker, &idleDeadlineListener{Listener: listener, deadline: idleDeadline})

	conn := dialBroker(t, socket)

	require.Equal(t, http.StatusOK, roundTrip(t, conn, http.MethodGet, "/health", ""))

	select {
	case remaining := <-idleDeadline:
		require.Greater(t, remaining, 2500*time.Millisecond)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start waiting for the next request")
	}

	require.Equal(t, http.StatusOK, roundTrip(t, conn, http.MethodGet, "/health", ""))
}

type idleDeadlineListener struct {
	net.Listener

	deadline chan<- time.Duration
}

func (l *idleDeadlineListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, fmt.Errorf("accept keep-alive connection: %w", err)
	}

	return &idleDeadlineConn{Conn: conn, deadline: l.deadline}, nil
}

type idleDeadlineConn struct {
	net.Conn

	deadline  chan<- time.Duration
	responded bool
}

func (c *idleDeadlineConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)

	c.responded = true

	if err != nil {
		return n, fmt.Errorf("write response: %w", err)
	}

	return n, nil
}

func (c *idleDeadlineConn) SetReadDeadline(t time.Time) error {
	if c.responded && c.deadline != nil && t.After(time.Now()) {
		c.deadline <- time.Until(t)

		c.deadline = nil
	}

	if err := c.Conn.SetReadDeadline(t); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}

	return nil
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

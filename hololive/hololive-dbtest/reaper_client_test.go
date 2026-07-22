package dbtest

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConnectSessionReaperRetriesReplacementWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	lookupCalls := 0
	registerCalls := 0
	lookup := func(context.Context) (sessionReaper, bool, error) {
		lookupCalls++
		if lookupCalls == 1 {
			return sessionReaper{}, false, nil
		}
		return sessionReaper{endpoint: "reaper:8080"}, true, nil
	}
	register := func(context.Context, sessionReaper) (net.Conn, error) {
		registerCalls++
		if registerCalls == 1 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNRESET}
		}

		client, server := net.Pipe()
		t.Cleanup(func() {
			require.NoError(t, client.Close())
			require.NoError(t, server.Close())
		})
		return client, nil
	}

	conn, err := connectSessionReaper(ctx, lookup, register)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 3, lookupCalls)
	require.Equal(t, 2, registerCalls)
}

func TestConnectSessionReaperDoesNotRetryProtocolError(t *testing.T) {
	wantErr := errors.New("unexpected reaper response")
	registerCalls := 0

	_, err := connectSessionReaper(
		context.Background(),
		func(context.Context) (sessionReaper, bool, error) {
			return sessionReaper{endpoint: "reaper:8080"}, true, nil
		},
		func(context.Context, sessionReaper) (net.Conn, error) {
			registerCalls++
			return nil, wantErr
		},
	)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, 1, registerCalls)
}

func TestConnectSessionReaperTimesOutWhenReplacementNeverAppears(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := connectSessionReaper(
		ctx,
		func(context.Context) (sessionReaper, bool, error) {
			return sessionReaper{}, false, nil
		},
		func(context.Context, sessionReaper) (net.Conn, error) {
			t.Fatal("register must not run without a reaper")
			return nil, nil
		},
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestReaperSessionFilterLineUsesCurrentSessionAndOptionalVersion(t *testing.T) {
	line := reaperSessionFilterLine("0.13.0")

	require.True(t, strings.HasSuffix(line, "\n"))
	require.Contains(t, line, "label=org.testcontainers=true")
	require.Contains(t, line, "label=org.testcontainers.lang=go")
	require.Contains(t, line, "label=org.testcontainers.reap=true")
	require.Contains(t, line, "label=org.testcontainers.sessionId=")
	require.Contains(t, line, "label=org.testcontainers.version=0.13.0")
	require.NotContains(t, reaperSessionFilterLine(""), reaperVersionLabel+"=")
}

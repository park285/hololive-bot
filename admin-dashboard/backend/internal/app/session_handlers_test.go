package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
)

type heartbeatBody struct {
	io.Reader
	closeErr error
	closed   bool
}

func (b *heartbeatBody) Close() error {
	b.closed = true
	return b.closeErr
}

func heartbeatRequestWithBody(body string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(body))
}

func TestParseHeartbeatAllowsEmptyBody(t *testing.T) {
	hb, err := parseHeartbeat(heartbeatRequestWithBody(" \n\t"))
	require.NoError(t, err)
	require.False(t, hb.Idle)
}

func TestParseHeartbeatAcceptsEmptyObject(t *testing.T) {
	hb, err := parseHeartbeat(heartbeatRequestWithBody(`{}`))
	require.NoError(t, err)
	require.False(t, hb.Idle)
}

func TestParseHeartbeatRejectsNullBody(t *testing.T) {
	_, err := parseHeartbeat(heartbeatRequestWithBody(`null`))
	require.Error(t, err)
}

func TestParseHeartbeatRejectsNullIdle(t *testing.T) {
	_, err := parseHeartbeat(heartbeatRequestWithBody(`{"idle":null}`))
	require.Error(t, err)
}

func TestParseHeartbeatAcceptsExactLimit(t *testing.T) {
	payload := `{"idle":true}`
	payload += strings.Repeat(" ", int(maxHeartbeatBodyBytes)-len(payload))

	hb, err := parseHeartbeat(heartbeatRequestWithBody(payload))
	require.NoError(t, err)
	require.True(t, hb.Idle)
}

func TestParseHeartbeatRejectsOversizedValidPrefix(t *testing.T) {
	payload := `{"idle":true}`
	payload += strings.Repeat(" ", int(maxHeartbeatBodyBytes)-len(payload)+1)

	_, err := parseHeartbeat(heartbeatRequestWithBody(payload))
	require.ErrorContains(t, err, "heartbeat body exceeds")
}

func TestParseHeartbeatRejectsUnknownFields(t *testing.T) {
	_, err := parseHeartbeat(heartbeatRequestWithBody(`{"idle":false,"unexpected":true}`))
	require.Error(t, err)
}

func TestParseHeartbeatRejectsMultipleValues(t *testing.T) {
	_, err := parseHeartbeat(heartbeatRequestWithBody(`{"idle":false}{"idle":true}`))
	require.ErrorContains(t, err, "multiple json values")
}

func TestParseHeartbeatReturnsBodyCloseError(t *testing.T) {
	closeErr := errors.New("close heartbeat body")
	body := &heartbeatBody{Reader: strings.NewReader(`{}`), closeErr: closeErr}

	_, err := parseHeartbeat(&http.Request{Body: body})

	require.ErrorIs(t, err, closeErr)
	require.True(t, body.closed)
}

func TestParseHeartbeatPrefersReadErrorOverCloseError(t *testing.T) {
	readErr := errors.New("read heartbeat body")
	closeErr := errors.New("close heartbeat body")
	body := &heartbeatBody{Reader: iotest.ErrReader(readErr), closeErr: closeErr}

	_, err := parseHeartbeat(&http.Request{Body: body})

	require.ErrorIs(t, err, readErr)
	require.NotErrorIs(t, err, closeErr)
	require.True(t, body.closed)
}

func TestWaitForLoginBackoffStopsWhenRequestIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.False(t, waitForLoginBackoff(ctx, time.Hour))
}

func TestWaitForLoginBackoffSkipsNonPositiveDelay(t *testing.T) {
	require.True(t, waitForLoginBackoff(context.Background(), 0))
}

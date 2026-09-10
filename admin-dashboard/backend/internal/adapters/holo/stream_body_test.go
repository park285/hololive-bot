package holo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/stretchr/testify/require"
)

const (
	streamContentTypeHeader = "Content-Type"
	streamJSONMediaType     = "application/json"
)

type measuredBody struct {
	reader   io.Reader
	closeErr error
	read     int64
	closed   bool
}

func (b *measuredBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)

	b.read += int64(n)

	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}

	if err != nil {
		return n, fmt.Errorf("read fixture stream: %w", err)
	}

	return n, nil
}

func (b *measuredBody) Close() error {
	b.closed = true

	return b.closeErr
}

func TestStreamedResponseHonorsLimitAndCloses(t *testing.T) {
	for _, extra := range []int64{0, 4096} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			body := &measuredBody{reader: io.MultiReader(strings.NewReader(`"`), io.LimitReader(infiniteByteReader{}, maxProxyBodyBytes-2+extra), strings.NewReader(`"`))}
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{streamContentTypeHeader: {streamJSONMediaType}}, Body: body}
			err := decodeOwnedBody(response, http.StatusOK, new(string))

			if extra == 0 {
				require.NoError(t, err)
				require.EqualValues(t, maxProxyBodyBytes, body.read)
			} else {
				require.ErrorIs(t, err, httputil.ErrResponseBodyTooLarge)
				require.LessOrEqual(t, body.read, int64(maxProxyBodyBytes+1)+responseBodyDrainLimit+1)
			}

			require.True(t, body.closed)
		})
	}
}

func TestStreamedResponseIncludesTrailingWhitespaceAndRejectsAdditionalJSON(t *testing.T) {
	for _, suffix := range []string{strings.Repeat(" ", maxProxyBodyBytes), ` {"private":"synthetic-private-value"}`} {
		body := &measuredBody{reader: strings.NewReader(`{}` + suffix)}
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{streamContentTypeHeader: {streamJSONMediaType}}, Body: body}
		err := decodeOwnedBody(response, http.StatusOK, new(map[string]any))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "synthetic-private-value")
		require.True(t, body.closed)
	}
}

type failedStream struct{ err error }

func (r failedStream) Read([]byte) (int, error) { return 0, r.err }

func TestStreamedResponsePreservesIOAndCloseFailures(t *testing.T) {
	closeErr := errors.New("fixture close failure")
	body := &measuredBody{reader: io.MultiReader(strings.NewReader(`{"status":"`), failedStream{context.Canceled}), closeErr: closeErr}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{streamContentTypeHeader: {streamJSONMediaType}}, Body: body}
	err := decodeOwnedBody(response, http.StatusOK, new(map[string]any))
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, closeErr)
	require.True(t, body.closed)
}

func TestStreamedResponseDoesNotHideSuccessfulBodyCloseFailure(t *testing.T) {
	closeErr := errors.New("fixture close failure")
	body := &measuredBody{reader: strings.NewReader(`{"status":"ok"}`), closeErr: closeErr}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{streamContentTypeHeader: {streamJSONMediaType}}, Body: body}
	err := decodeOwnedBody(response, http.StatusOK, new(map[string]any))
	require.ErrorIs(t, err, closeErr)
	require.True(t, body.closed)
}

type onceFailedStream struct {
	data []byte
	err  error
}

func (r *onceFailedStream) Read(p []byte) (int, error) {
	if len(r.data) == 0 && r.err == nil {
		return 0, io.EOF
	}

	n := copy(p, r.data)

	r.data = r.data[n:]

	if len(r.data) != 0 {
		return n, nil
	}

	err := r.err

	r.err = nil

	return n, err
}

func TestProjectedResponsesPreserveReadFailuresAtEveryBoundary(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		target func() any
	}{
		{
			name:   "members",
			body:   ` {"status":"ok","members":[{"id":123,"channelId":"channel","name":"synthetic-private-value","isGraduated":false,"aliases":{"ko":["별명"],"ja":[]},"extra":{"value":"ignored"}}]}`,
			target: func() any { return new(MembersResponse) },
		},
		{
			name:   "alarms",
			body:   ` {"status":"ok","extra":{"value":"ignored"},"alarms":[{"roomId":"123","roomName":"synthetic-private-value","channelId":"channel","memberName":"member","extra":["ignored"]}]}`,
			target: func() any { return new(AlarmsResponse) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for cut := range len(tc.body) + 1 {
				// drain에서 같은 오류가 재발하면 decoder의 원인 유실을 가리므로 한 번만 실패합니다.
				for _, closeErr := range []error{nil, errors.New("fixture close failure")} {
					readers := []io.Reader{
						io.MultiReader(strings.NewReader(tc.body[:cut]), &onceFailedStream{err: context.Canceled}),
						&onceFailedStream{data: []byte(tc.body[:cut]), err: context.Canceled},
					}
					for _, reader := range readers {
						assertProjectedReadFailure(t, reader, closeErr, tc.target(), cut)
					}
				}
			}
		})
	}
}

func assertProjectedReadFailure(t *testing.T, reader io.Reader, closeErr error, target any, cut int) {
	t.Helper()

	body := &measuredBody{reader: reader, closeErr: closeErr}
	response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{streamContentTypeHeader: {streamJSONMediaType}}, Body: body}
	err := decodeOwnedBody(response, http.StatusOK, target)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled, "read boundary %d", cut)

	if closeErr != nil {
		require.ErrorIs(t, err, closeErr, "read boundary %d", cut)
	}

	require.NotContains(t, err.Error(), "synthetic-private-value")
	require.True(t, body.closed)
}

package geminidecision

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testKey = "test-key-0123456789abcdef"

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &Client{http: server.Client(), endpoint: server.URL, key: testKey}
}

func TestDecideSendsInputsAndQuestions(t *testing.T) {
	t.Parallel()

	type captured struct {
		key  string
		body []byte
	}

	requests := make(chan captured, 1)

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}

		requests <- captured{key: r.Header.Get("x-goog-api-key"), body: body}

		writeBody(t, w, `{"answers":{"kind":{"type":"choice","choice":"b","probabilities":{"a":0.2,"b":0.8},
			"confidence":0.8}},"usage":{"total_input_tokens":321,"total_output_tokens":0}}`)
	})

	response, err := client.Decide(t.Context(), Request{
		Input: []Input{
			{Type: "text", Text: "hello"},
			{Type: "image", MIMEType: "image/jpeg", Data: []byte{0xff, 0xd8}, Resolution: "medium"},
		},
		Questions: map[string]Question{"kind": {Type: "choice", Instructions: "Which?", Criteria: map[string]string{"a": "A", "b": "B"}}},
	})
	require.NoError(t, err)

	request := <-requests
	require.Equal(t, testKey, request.key)

	var received map[string]any

	require.NoError(t, jsonv2.Unmarshal(request.body, &received))
	require.Equal(t, map[string]any{
		"model": Model,
		"input": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "image", "mime_type": "image/jpeg", "data": "/9g=", "resolution": "medium"},
		},
		"questions": map[string]any{
			"kind": map[string]any{"type": "choice", "instructions": "Which?", "criteria": map[string]any{"a": "A", "b": "B"}},
		},
	}, received)

	answer := response.Answers["kind"]
	require.Equal(t, "b", *answer.Choice)
	require.InDelta(t, 0.8, *answer.Confidence, 1e-9)
	require.Equal(t, map[string]float64{"a": 0.2, "b": 0.8}, answer.Probabilities)
	require.Equal(t, int32(321), response.Usage.TotalInputTokens)
}

func TestDecideSendsChoicesInTheSameOrderEveryTime(t *testing.T) {
	t.Parallel()

	var bodies []string

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}

		bodies = append(bodies, string(body))

		writeBody(t, w, `{"answers":{}}`)
	})

	criteria := map[string]string{}

	for _, key := range []string{"talk", "game", "singing", "asmr", "membership", "event", "news", "other", "watchalong", "horse_racing"} {
		criteria[key] = key + " stream"
	}

	request := Request{Questions: map[string]Question{"type": {Type: "choice", Instructions: "Which?", Criteria: criteria}}}

	for range 20 {
		_, err := client.Decide(t.Context(), request)
		require.NoError(t, err)
	}

	for _, body := range bodies {
		require.Equal(t, bodies[0], body)
	}
}

func TestDecideClassifiesFailures(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"safety block": {http.StatusBadRequest, `{"error":{"code":"safety","message":"Request blocked due to safety violations"}}`, ErrBlocked},
		"image safety": {http.StatusBadRequest, `{"error":{"code":"image_safety","message":"blocked"}}`, ErrBlocked},
		"prompt feedback": {
			http.StatusOK, `{"prompt_feedback":{"block_reason":"SAFETY"},"usage":{"total_input_tokens":10}}`, ErrBlocked,
		},
		"invalid request": {http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"Unable to process input image."}}`, ErrRejected},
		"invalid key": {http.StatusBadRequest, `{"error":{"code":400,"message":"API key not valid.","status":"INVALID_ARGUMENT",
			"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`, ErrUnavailable},
		"unsupported location": {
			http.StatusBadRequest, `{"error":{"code":400,"message":"User location is not supported.","status":"FAILED_PRECONDITION"}}`, ErrUnavailable,
		},
		"rate limited": {http.StatusTooManyRequests, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, ErrUnavailable},
		"forbidden":    {http.StatusForbidden, `{"error":{"code":403,"status":"PERMISSION_DENIED"}}`, ErrUnavailable},
		"unavailable":  {http.StatusServiceUnavailable, ``, ErrUnavailable},
		"server error": {http.StatusInternalServerError, `{"error":{"code":500,"status":"INTERNAL"}}`, nil},
		"broken body":  {http.StatusOK, `{"answers":`, nil},
	} {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			writeBody(t, w, tc.body)
		})

		_, err := client.Decide(t.Context(), Request{})
		require.Error(t, err, name)

		for _, sentinel := range []error{ErrBlocked, ErrRejected, ErrUnavailable} {
			require.Equal(t, errors.Is(tc.want, sentinel), errors.Is(err, sentinel), "%s: %v", name, err)
		}
	}
}

func TestDecideTreatsConnectFailureAsUnavailableAndResponseTimeoutAsUnknown(t *testing.T) {
	t.Parallel()

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	_, err := (&Client{http: &http.Client{}, endpoint: closedURL, key: testKey}).Decide(t.Context(), Request{})
	require.ErrorIs(t, err, ErrUnavailable)

	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hang.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = (&Client{http: hang.Client(), endpoint: hang.URL, key: testKey}).Decide(ctx, Request{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnavailable)

	guarded := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()

		return nil, fmt.Errorf("netguard: round trip: %w", request.Context().Err())
	})}

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = (&Client{http: guarded, endpoint: hang.URL, key: testKey}).Decide(ctx, Request{})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnavailable)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestLoadAPIKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

		return path
	}

	key, err := LoadAPIKey(write("key", testKey+"\n"))
	require.NoError(t, err)
	require.Equal(t, testKey, key)

	const authorizationKey = "AQ.test-key_0123456789abcdef0123456789abcdef01234567"

	key, err = LoadAPIKey(write("authorization-key", authorizationKey+"\n"))
	require.NoError(t, err)
	require.Equal(t, authorizationKey, key)

	for _, content := range []string{"GEMINI_API_KEY=" + testKey, "short", testKey + " " + testKey} {
		_, err = LoadAPIKey(write("bad", content))
		require.Error(t, err)
		require.NotContains(t, err.Error(), testKey)
	}

	_, err = LoadAPIKey("relative/key")
	require.Error(t, err)
}

func writeBody(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()

	if _, err := w.Write([]byte(body)); err != nil {
		t.Error(err)
	}
}

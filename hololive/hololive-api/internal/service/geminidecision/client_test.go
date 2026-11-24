package geminidecision

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai/interactions/models/decisions"
	"google.golang.org/genai/interactions/models/interactions"
)

const testKey = "test-key-0123456789abcdef"

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))

	return NewWithHTTPClient(testKey, server.Client())
}

func TestDecideSendsInputsAndQuestions(t *testing.T) {
	t.Parallel()

	type captured struct {
		key  string
		body []byte
	}

	requests := make(chan captured, 1)

	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != apiHost || r.Method != http.MethodPost || r.URL.Path != "/v1beta/decisions" {
			t.Errorf("unexpected decision request %s %s", r.Method, r.URL.Path)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}

		requests <- captured{key: r.Header.Get("x-goog-api-key"), body: body}

		writeBody(t, w, `{"answers":{"kind":{"type":"choice","choice":"b","probabilities":{"a":0.2,"b":0.8},
			"confidence":0.8}},"usage":{"total_input_tokens":321,"total_output_tokens":0}}`)
	})

	response, err := client.Decide(t.Context(), Request{
		Input: decisions.NewInput([]decisions.DecisionInputContent{
			Text("hello"),
			Image("image/jpeg", []byte{0xff, 0xd8}, new(interactions.MediaResolutionMedium)),
		}),
		Questions: map[string]decisions.Question{"kind": ChoiceQuestion("Which?", map[string]string{"a": "A", "b": "B"})},
	})
	require.NoError(t, err)

	request := <-requests
	require.Equal(t, testKey, request.key)

	want := []byte(`{"model":"gemini-decision-1","input":[{"type":"text","text":"hello"},{"type":"image","mime_type":"image/jpeg","data":"/9g=","resolution":"medium"}],` +
		`"questions":{"kind":{"type":"choice","instructions":"Which?","criteria":{"a":"A","b":"B"}}}}`)
	require.Equal(t, want, request.body)

	answer := response.Answers["kind"]
	require.Equal(t, "b", *answer.Choice)
	require.InDelta(t, 0.8, *answer.Confidence, 1e-9)
	require.Equal(t, map[string]float64{"a": 0.2, "b": 0.8}, answer.Probabilities)
	require.Equal(t, int32(321), response.Usage.TotalInputTokens)
}

func TestDecideDistinguishesMissingConfidenceFromZero(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeBody(t, w, `{"answers":{"missing":{"type":"choice","choice":"a"},"zero":{"type":"choice","choice":"a","confidence":0}}}`)
	})
	response, err := client.Decide(t.Context(), validRequest())
	require.NoError(t, err)
	require.Nil(t, response.Answers["missing"].Confidence)
	require.Equal(t, new(0.0), response.Answers["zero"].Confidence)
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

	request := Request{Input: decisions.NewInput("stream <&>"), Questions: map[string]decisions.Question{"type": ChoiceQuestion("Which?", criteria)}}
	sortedChoices, err := jsonv2.Marshal(criteria, jsonv2.Deterministic(true))
	require.NoError(t, err)

	want := `{"model":"gemini-decision-1","input":"stream <&>","questions":{"type":{"type":"choice","instructions":"Which?","criteria":` + string(sortedChoices) + "}}}"

	for range 20 {
		_, err := client.Decide(t.Context(), request)
		require.NoError(t, err)
	}

	for _, body := range bodies {
		require.Equal(t, want, body)
	}
}

func TestDecideClassifiesFailures(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
		want   error
	}{
		"safety block":      {http.StatusBadRequest, `{"error":{"code":"safety","message":"Request blocked due to safety violations"}}`, ErrBlocked},
		"image safety":      {http.StatusBadRequest, `{"error":{"code":"image_safety","message":"blocked"}}`, ErrBlocked},
		"malformed details": {http.StatusBadRequest, `{"error":{"code":"safety","details":42}}`, ErrRejected},
		"prompt feedback": {
			http.StatusOK, `{"prompt_feedback":{"block_reason":"api_key:` + testKey + `"},"usage":{"total_input_tokens":10}}`, ErrBlocked,
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
		"created":      {http.StatusCreated, `{"answers":{}}`, nil},
	} {
		client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			writeBody(t, w, tc.body)
		})

		_, err := client.Decide(t.Context(), validRequest())
		require.Error(t, err, name)
		requireNoKeyInDiagnostics(t, err)

		for _, sentinel := range []error{ErrBlocked, ErrRejected, ErrUnavailable} {
			require.Equal(t, errors.Is(tc.want, sentinel), errors.Is(err, sentinel), "%s: %v", name, err)
		}
	}
}

func TestDecideReturnsOnlyClassificationDiagnostics(t *testing.T) {
	t.Parallel()

	const consumer = "Consumer 'api_key:" + testKey + "' is suspended"

	for _, tc := range []struct {
		name    string
		status  int
		payload string
		want    error
		detail  string
	}{
		{"suspended", http.StatusForbidden, `{"error":{"code":403,"status":"PERMISSION_DENIED","message":"` + consumer +
			`","details":[{"reason":"CONSUMER_SUSPENDED","metadata":{"consumer":"api_key:` + testKey + `"}}]}}`, ErrUnavailable, "decision status 403"},
		{"unknown fields", http.StatusBadRequest, `{"error":{"code":"` + testKey + `","status":"` + testKey +
			`","details":[{"reason":"` + testKey + `"}]}}`, ErrRejected, "decision status 400"},
		{"safety", http.StatusBadRequest, `{"error":{"code":"image_safety","message":"` + consumer + `"}}`, ErrBlocked, "decision status 400 image_safety"},
		{"precondition", http.StatusBadRequest, `{"error":{"status":"FAILED_PRECONDITION","message":"` + consumer + `"}}`, ErrUnavailable, "decision status 400 FAILED_PRECONDITION"},
		{"invalid key", http.StatusBadRequest, `{"error":{"details":[{"reason":"API_KEY_INVALID"},{"reason":"` + testKey + `"}]}}`, ErrUnavailable, "decision status 400 API_KEY_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != "/v1beta/decisions" || r.Header.Get("x-goog-api-key") != testKey {
					t.Error("unexpected decision request URL or authentication header")
				}

				w.WriteHeader(tc.status)
				writeBody(t, w, tc.payload)
			})
			_, err := client.Decide(t.Context(), validRequest())
			require.ErrorIs(t, err, tc.want)

			status, ok := errors.AsType[*StatusError](err)
			require.True(t, ok)
			require.EqualError(t, status, tc.detail)
			requireNoKeyInDiagnostics(t, err)
		})
	}
}

func requireNoKeyInDiagnostics(t *testing.T, err error) {
	t.Helper()

	var logged bytes.Buffer

	slog.New(slog.NewJSONHandler(&logged, nil)).ErrorContext(t.Context(), "Gemini request failed", slog.Any("error", err))
	require.NotContains(t, err.Error(), testKey)
	require.NotContains(t, err.Error(), "api_key:")
	require.NotContains(t, logged.String(), testKey)
	require.NotContains(t, logged.String(), "api_key:")
}

func TestDecideDoesNotExposeAnInvalidRedirectLocation(t *testing.T) {
	t.Parallel()

	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/%zz?consumer=api_key:"+testKey)
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	_, err := client.Decide(t.Context(), validRequest())

	status, ok := errors.AsType[*StatusError](err)
	require.True(t, ok)
	require.Equal(t, http.StatusTemporaryRedirect, status.HTTPStatus)
	requireNoKeyInDiagnostics(t, err)
}

func TestDecideDropsCredentialsFromWrappedTransportErrors(t *testing.T) {
	t.Parallel()

	client := NewWithHTTPClient(testKey, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.Header.Get("x-goog-api-key")

		return nil, &url.Error{
			Op: http.MethodPost, URL: baseURL + "/?key=" + key,
			Err: fmt.Errorf("api_key:%s: %w", key, syscall.ECONNRESET),
		}
	})})

	_, err := client.Decide(t.Context(), validRequest())
	require.ErrorIs(t, err, ErrUnavailable)
	require.ErrorIs(t, err, syscall.ECONNRESET)

	urlErr, ok := errors.AsType[*url.Error](err)
	require.True(t, ok)
	require.Equal(t, baseURL, urlErr.URL)
	requireNoKeyInDiagnostics(t, urlErr.Err)
	requireNoKeyInDiagnostics(t, err)
}

func TestDecideTreatsConnectFailureAsUnavailableAndResponseTimeoutAsUnknown(t *testing.T) {
	t.Parallel()

	closed := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, syscall.ECONNREFUSED
	})}

	_, err := NewWithHTTPClient(testKey, closed).Decide(t.Context(), validRequest())
	require.ErrorIs(t, err, ErrUnavailable)

	release := make(chan struct{})
	hang := httptest.NewTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = NewWithHTTPClient(testKey, hang.Client()).Decide(ctx, validRequest())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnavailable)

	guarded := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()

		return nil, fmt.Errorf("netguard: round trip: %w", request.Context().Err())
	})}

	ctx, cancel = context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err = NewWithHTTPClient(testKey, guarded).Decide(ctx, validRequest())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnavailable)
}

func validRequest() Request {
	return Request{Input: decisions.NewInput("post"), Questions: map[string]decisions.Question{"schedule": NoulQuestion("Schedule?", "Yes", "No")}}
}

func TestDecideDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			var origin, target atomic.Int32

			server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host == apiHost && r.URL.Path == "/v1beta/decisions" && r.Method == http.MethodPost {
					origin.Add(1)
					http.Redirect(w, r, "https://redirect.example/elsewhere", code)

					return
				}

				target.Add(1)
				w.WriteHeader(http.StatusTeapot)
			}))
			client := server.Client()
			require.Nil(t, client.CheckRedirect)

			_, err := NewWithHTTPClient(testKey, client).Decide(t.Context(), validRequest())
			require.EqualValues(t, 1, origin.Load())
			require.Zero(t, target.Load())
			require.Nil(t, client.CheckRedirect)

			status, ok := errors.AsType[*StatusError](err)
			require.True(t, ok)
			require.Equal(t, code, status.HTTPStatus)
		})
	}
}

func TestDecideMakesOneAttempt(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"503", "connection reset"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			attempts := 0
			client := NewWithHTTPClient(testKey, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				attempts++

				if name == "connection reset" {
					return nil, &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
				}

				return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
			})})

			_, err := client.Decide(t.Context(), validRequest())
			require.Equal(t, 1, attempts)
			require.ErrorIs(t, err, ErrUnavailable)
		})
	}
}

func TestDecideDoesNotReplayRefusedHTTP2Stream(t *testing.T) {
	t.Parallel()

	type connKey struct{}

	var posts atomic.Int32

	release := make(chan struct{})
	defer close(release)

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.TLS == nil || r.Method != http.MethodPost || r.URL.Path != "/v1beta/decisions" {
			t.Errorf("unexpected decision request %s %s %s", r.Proto, r.Method, r.URL.Path)

			return
		}

		if posts.Add(1) != 1 {
			w.Header().Set("Content-Type", "application/json")
			writeBody(t, w, `{"answers":{}}`)

			return
		}

		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)

			return
		}

		conn, ok := r.Context().Value(connKey{}).(net.Conn)
		if !ok {
			t.Error("missing TLS connection")

			return
		}

		const rstStream, refusedStream = 3, 7

		resetFirstStream := []byte{0, 0, 4, rstStream, 0, 0, 0, 0, 1, 0, 0, 0, refusedStream}

		if _, err := conn.Write(resetFirstStream); err != nil {
			t.Error(err)

			return
		}

		<-release
	}))

	server.EnableHTTP2 = true
	server.Config.ConnContext = func(ctx context.Context, conn net.Conn) context.Context {
		return context.WithValue(ctx, connKey{}, conn)
	}

	_, err := NewWithHTTPClient(testKey, server.Client()).Decide(t.Context(), validRequest())
	require.EqualValues(t, 1, posts.Load())
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestDecideCapsResponseBeforeDecode(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		for _, size := range []int{maxResponseBytes, maxResponseBytes + 1, 2 * maxResponseBytes} {
			t.Run(fmt.Sprintf("%d/%d", status, size), func(t *testing.T) {
				t.Parallel()

				body := strings.NewReader("{}" + strings.Repeat(" ", size-2))
				client := NewWithHTTPClient(testKey, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(body), ContentLength: -1, Request: req,
					}, nil
				})})

				_, err := client.Decide(t.Context(), validRequest())

				switch {
				case size > maxResponseBytes:
					require.ErrorIs(t, err, httputil.ErrResponseBodyTooLarge)
					require.Equal(t, size-maxResponseBytes-1, body.Len())
				case status == http.StatusOK:
					require.NoError(t, err)
				default:
					require.ErrorIs(t, err, ErrRejected)
				}
			})
		}
	}
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

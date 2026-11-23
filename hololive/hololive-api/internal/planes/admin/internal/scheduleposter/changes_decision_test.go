package scheduleposter

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/service/geminidecision"
	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const decisionTestKey = "test-key-0123456789abcdef"

func testDecisionJudge(client *http.Client) *geminiJudge {
	return &geminiJudge{decisions: geminidecision.NewWithHTTPClient(decisionTestKey, client)}
}

func refusedDecisionClient() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}}}
}

func readChangeFixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := fs.ReadFile(os.DirFS(filepath.Join("testdata", "changes")), name)
	require.NoError(t, err)

	return raw
}

func decisionServer(t *testing.T, status int, body string) (*geminiJudge, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls.Add(1)
		w.WriteHeader(status)

		if _, err := io.WriteString(w, body); err != nil {
			t.Error(err)
		}
	}))

	return testDecisionJudge(server.Client()), &calls
}

func TestClassifyPostSendsTheBodyAsOneStringWithThePrototypeQuestions(t *testing.T) {
	t.Parallel()

	const body = "明日は21時から歌枠！\n\"引用\" と改行"

	want := jsontext.Value(readChangeFixture(t, "decide_questions.json"))
	require.NoError(t, want.Compact())

	var received struct {
		Model     string         `json:"model"`
		Input     jsontext.Value `json:"input"`
		Questions jsontext.Value `json:"questions"`
	}

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path != "/v1beta/decisions" || r.Method != http.MethodPost || r.Header.Get("x-goog-api-key") != decisionTestKey ||
			jsonv2.UnmarshalRead(r.Body, &received, jsonv2.RejectUnknownMembers(true)) != nil {
			w.WriteHeader(http.StatusTeapot)

			return
		}

		if _, err := io.WriteString(w, `{"answers":{"schedule_info":{"type":"noul","noul":0.91,"confidence":0.9},`+
			`"kind":{"type":"choice","choice":"announce","probabilities":{"announce":0.8}}},"usage":{"total_input_tokens":212}}`); err != nil {
			t.Error(err)
		}
	}))

	judge := testDecisionJudge(server.Client())

	answer, spent, err := judge.classifyPost(t.Context(), body)
	require.NoError(t, err)
	require.Equal(t, postAnswer{ScheduleInfo: 0.91, Kind: "announce"}, answer)
	require.Equal(t, usage{model: decisionModel, prompt: 212}, spent)

	require.Equal(t, decisionModel, received.Model)
	require.Equal(t, jsontext.Kind('"'), received.Input.Kind(), "input은 배열이 아니라 문자열 하나다")

	var input string

	require.NoError(t, jsonv2.Unmarshal(received.Input, &input))
	require.Equal(t, body, input)

	require.NoError(t, received.Questions.Compact())
	require.Equal(t, want, received.Questions)
}

const (
	failedByPost    = "post"
	failedByService = "service"
	failedUnknown   = "unknown"
)

func classifyFailure(t *testing.T, status int, body string) (string, string) {
	t.Helper()

	judge, calls := decisionServer(t, status, body)

	_, _, err := judge.classifyPost(t.Context(), "本文")
	require.Error(t, err)
	require.EqualValues(t, 1, calls.Load(), "판별은 호출 안에서 다시 보내지 않는다")

	fault, isFault := errors.AsType[*postFaultError](err)
	service, unknown := errors.Is(err, errChangeServiceFailed), errors.Is(err, errChangeOutcomeUnknown)

	switch {
	case isFault && !service && !unknown:
		return failedByPost, fault.reason
	case service && !isFault && !unknown:
		return failedByService, ""
	case unknown && !isFault && !service:
		return failedUnknown, ""
	default:
		return "", err.Error()
	}
}

func TestClassifyPostReadsOnlyThePostAnswers(t *testing.T) {
	t.Parallel()

	judge, _ := decisionServer(t, http.StatusOK, `{"answers":{"schedule_info":{"type":"noul","noul":0,"confidence":"unused"},
		"kind":{"type":"choice","choice":"cancel"}},"usage":{"total_input_tokens":17},"prompt_feedback":{"block_reason_message":42}}`)
	answer, spent, err := judge.classifyPost(t.Context(), "本文")
	require.NoError(t, err)
	require.Equal(t, postAnswer{ScheduleInfo: 0, Kind: "cancel"}, answer)
	require.Equal(t, usage{model: decisionModel, prompt: 17}, spent)
}

func TestClassifyPostClosesFailuresCausedByThePost(t *testing.T) {
	t.Parallel()

	for body, reason := range map[string]string{
		`{"prompt_feedback":{"block_reason":"SAFETY"},"usage":{"total_input_tokens":10}}`:          reasonBlocked,
		`{"answers":{"schedule_info":{"type":"noul"},"kind":{"type":"choice","choice":"cancel"}}}`: reasonBadAnswer,
		`{"answers":{"schedule_info":{"noul":0.9}}}`:                                               reasonBadAnswer,
		`{"answers":{"kind":{"choice":"change"}}}`:                                                 reasonBadAnswer,
		`{"answers":{"schedule_info":{"noul":1.2},"kind":{"choice":"change"}}}`:                    reasonBadAnswer,
		`{"answers":{"schedule_info":{"noul":-0.1},"kind":{"choice":"change"}}}`:                   reasonBadAnswer,
		`{"answers":{"schedule_info":{"noul":0.9},"kind":{"choice":"maybe"}}}`:                     reasonBadAnswer,
		`{"answers":`: reasonBadAnswer,
	} {
		kind, got := classifyFailure(t, http.StatusOK, body)
		require.Equal(t, failedByPost, kind, body)
		require.Equal(t, reason, got, body)
	}

	for _, code := range []string{"safety", "prohibited_content", "blocklist", "image_safety", "content_blocked"} {
		kind, got := classifyFailure(t, http.StatusBadRequest, `{"error":{"code":"`+code+`","message":"blocked"}}`)
		require.Equal(t, failedByPost, kind, code)
		require.Equal(t, reasonBlocked, got, "글 reason은 계약 enum이라 안전 차단 코드는 blocked로 닫는다")
	}

	kind, reason := classifyFailure(t, http.StatusBadRequest, `{"error":{"code":"safety","details":42}}`)
	require.Equal(t, failedByPost, kind)
	require.Equal(t, reasonBlocked, reason)
}

func TestClassifyPostSeparatesServiceFromUnknownFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"error":{"code":"invalid_request","message":"bad"}}`, failedByService},
		{http.StatusBadRequest, `{"error":{"code":"quota_exceeded","message":"bad"}}`, failedByService},
		{http.StatusBadRequest, `{"error":{"code":400,"message":"API key not valid.","status":"INVALID_ARGUMENT"}}`, failedByService},
		{http.StatusBadRequest, `{"error":{"message":"bad"}}`, failedByService},
		{http.StatusBadRequest, `Bad Request`, failedByService},
		{http.StatusUnauthorized, `{"error":{"code":401}}`, failedByService},
		{http.StatusForbidden, `{"error":{"code":403,"status":"PERMISSION_DENIED"}}`, failedByService},
		{http.StatusNotFound, ``, failedByService},
		{http.StatusTooManyRequests, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, failedByService},
		{http.StatusServiceUnavailable, ``, failedByService},
		{http.StatusInternalServerError, `{"error":{"code":500,"status":"INTERNAL"}}`, failedUnknown},
		{http.StatusBadGateway, ``, failedUnknown},
		{http.StatusGatewayTimeout, ``, failedUnknown},
		{http.StatusOK, `{"answers":{"pad":"` + strings.Repeat("a", 64<<10) + `"}}`, failedUnknown},
	} {
		kind, detail := classifyFailure(t, tc.status, tc.body)
		require.Equal(t, tc.want, kind, "%d %.80s: %s", tc.status, tc.body, detail)
	}
}

func TestClassifyPostBlamesTheConnection(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	hang := httptest.NewTestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release) })

	plain := hang.Client()

	plain.Timeout = 100 * time.Millisecond

	_, _, err := testDecisionJudge(refusedDecisionClient()).classifyPost(t.Context(), "本文")
	require.ErrorIs(t, err, errChangeServiceFailed, "연결 실패는 어느 글도 판별할 수 없다")

	_, _, err = testDecisionJudge(plain).classifyPost(t.Context(), "本文")
	require.ErrorIs(t, err, errChangeOutcomeUnknown, "연결 뒤 시간 초과는 그 글 때문일 수도 있다")
	require.NotErrorIs(t, err, errChangeServiceFailed)

	dropped := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, readErr := io.Copy(io.Discard, r.Body); readErr != nil {
			return
		}

		conn, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Errorf("hijack decision connection: %v", hijackErr)

			return
		}

		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close hijacked decision connection: %v", closeErr)
		}
	}))

	_, _, err = testDecisionJudge(dropped.Client()).classifyPost(t.Context(), "本文")
	require.ErrorIs(t, err, errChangeOutcomeUnknown, "본문을 보낸 뒤 응답을 잃으면 결과를 알 수 없다")
	require.NotErrorIs(t, err, errChangeServiceFailed)
}

func TestPostAnswerDecisionCombinesScoreAndKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		answer postAnswer
		want   posterstore.PostStatus
	}{
		{postAnswer{ScheduleInfo: 0.7, Kind: "announce"}, posterstore.PostRelevant},
		{postAnswer{ScheduleInfo: 0.99, Kind: "change"}, posterstore.PostRelevant},
		{postAnswer{ScheduleInfo: 0.8, Kind: "cancel"}, posterstore.PostRelevant},
		{postAnswer{ScheduleInfo: 0.69, Kind: "change"}, posterstore.PostNotRelevant},
		{postAnswer{ScheduleInfo: 0.95, Kind: "other"}, posterstore.PostNotRelevant},
		{postAnswer{ScheduleInfo: 0.95, Kind: "weekly"}, posterstore.PostNotRelevant},
	} {
		require.Equal(t, posterstore.PostDecision{Status: tc.want, ScheduleInfo: tc.answer.ScheduleInfo, Kind: tc.answer.Kind},
			tc.answer.decision(0.7), "%+v", tc.answer)
	}
}

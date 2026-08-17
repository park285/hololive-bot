package youtubejs

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestRPC001RejectsErrorBodyWithHTTP200(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusOK,
		`{"protocol_version":1,"error":{"code":"collection_failed","class":"TRANSIENT","retry":{"kind":"default"},"message":"failed"}}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, err)
}

func TestRPC002RejectsImpossibleStatusTuple(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusServiceUnavailable,
		`{"protocol_version":1,"error":{"code":"parser_drift","class":"DATA_CONTRACT","retry":{"kind":"default"},"message":"drift"}}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, err)
}

func TestRPC003MapsCooldownRetryHint(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusTooManyRequests,
		`{"protocol_version":1,"error":{"code":"cooldown","class":"COOLDOWN","retry":{"kind":"after","after_ms":30000},"message":"limited"}}`,
		1<<20, &CommunityResult{})
	if collecterr.CodeOf(err) != collecterr.Cooldown || collecterr.ClassOf(err) != collecterr.ClassCooldown {
		t.Fatalf("code/class = %q/%q, error = %v", collecterr.CodeOf(err), collecterr.ClassOf(err), err)
	}
	hint := collecterr.RetryOf(err)
	if hint.Kind() != collecterr.RetryAfter || hint.After() != 30*time.Second {
		t.Fatalf("retry = %q/%s", hint.Kind(), hint.After())
	}
}

func TestRPC004RejectsUnknownSuccessField(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusOK,
		`{"protocol_version":1,"posts":[],"page_count":1,"exhausted":true,"continuity":"CONTIGUOUS","termination_reason":"exhausted","unknown":true}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, err)
}

func TestRPC005RejectsTrailingJSONValue(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusOK,
		`{"protocol_version":1,"posts":[],"page_count":1,"exhausted":true,"continuity":"CONTIGUOUS","termination_reason":"exhausted"} {}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, err)
}

func TestRPC006RejectsUnknownFailureVocabulary(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusBadGateway,
		`{"protocol_version":1,"error":{"code":"new_failure","class":"TRANSIENT","retry":{"kind":"default"},"message":"unknown"}}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, err)
}

func TestRPC007ContainsOversizedSuccess(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusOK, strings.Repeat("x", 65), 64, &CommunityResult{})
	if collecterr.CodeOf(err) != collecterr.ResponseTooLarge {
		t.Fatalf("code = %q, error = %v", collecterr.CodeOf(err), err)
	}
}

func TestRPC008BoundsOversizedError(t *testing.T) {
	t.Parallel()
	err := decodeTestResponse(t, http.StatusBadGateway, strings.Repeat("x", (8<<10)+1), 1<<20, &CommunityResult{})
	if err == nil {
		t.Fatal("expected oversized error response to fail")
	}
	assertProtocolMismatch(t, err)
	if strings.Contains(err.Error(), strings.Repeat("x", 128)) {
		t.Fatalf("oversized raw body leaked: %v", err)
	}
}

func TestPAG013RejectsMissingAndImpossibleTerminationReason(t *testing.T) {
	t.Parallel()
	missing := decodeTestResponse(t, http.StatusOK,
		`{"protocol_version":1,"posts":[],"page_count":1,"exhausted":true,"continuity":"CONTIGUOUS"}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, missing)

	impossible := decodeTestResponse(t, http.StatusOK,
		`{"protocol_version":1,"posts":[],"page_count":1,"exhausted":false,"continuity":"CONTIGUOUS","termination_reason":"exhausted"}`,
		1<<20, &CommunityResult{})
	assertProtocolMismatch(t, impossible)
}

func TestRPCMappingLockstepAndInternalInvariant(t *testing.T) {
	t.Parallel()
	lockstep := helperStatusError(http.StatusBadRequest, []byte(
		`{"protocol_version":1,"error":{"code":"invalid_request","class":"PROTOCOL","retry":{"kind":"default"},"message":"invalid"}}`,
	))
	assertProtocolMismatch(t, lockstep)
	internal := helperStatusError(http.StatusInternalServerError, []byte(
		`{"protocol_version":1,"error":{"code":"helper_internal_invariant","class":"INTERNAL","retry":{"kind":"default"},"message":"broken"}}`,
	))
	if collecterr.CodeOf(internal) != collecterr.Internal || collecterr.ClassOf(internal) != collecterr.ClassInternal {
		t.Fatalf("code/class = %q/%q, error = %v", collecterr.CodeOf(internal), collecterr.ClassOf(internal), internal)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func decodeTestResponse(t *testing.T, status int, body string, limit int64, response any) error {
	t.Helper()
	resp := jsonResponse(status, body)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response: %v", err)
		}
	}()
	return decodeHelperResponse(resp, limit, response)
}

func assertProtocolMismatch(t *testing.T, err error) {
	t.Helper()
	if collecterr.CodeOf(err) != collecterr.HelperProtocolMismatch || collecterr.ClassOf(err) != collecterr.ClassProtocol {
		t.Fatalf("code/class = %q/%q, error = %v", collecterr.CodeOf(err), collecterr.ClassOf(err), err)
	}
}

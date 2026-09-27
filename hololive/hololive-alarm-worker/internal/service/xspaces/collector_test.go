package xspaces

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectionOutputDiagnosticBoundary(t *testing.T) {
	_, err := decodeCollectionOutput([]byte(`{"error":"authentication","stage":"collect","http_status":403,"api_codes":[32]}`), errors.New("process failed"))
	failure, ok := errors.AsType[*CollectionError](err)

	if !ok || failure == nil {
		t.Fatalf("expected CollectionError, got %T", err)
	}

	require.Equal(t, "authentication", failure.Code)
	require.Equal(t, "collect", failure.Stage)
	require.Equal(t, 403, failure.HTTPStatus)
	require.Equal(t, []int{32}, failure.APICodes)
	require.Contains(t, failure.Error(), "stage=collect http_status=403 api_codes=[32]")

	for _, raw := range []string{
		`{"error":"authentication"}`,
		`{"error":"authentication_pending","stage":"collect"}`,
		`{"error":"authentication","stage":"collect","http_status":99}`,
		`{"error":"authentication","stage":"collect","http_status":600}`,
		`{"error":"authentication","stage":"collect","api_codes":[-1]}`,
		`{"error":"authentication","stage":"collect","api_codes":[65536]}`,
		`{"error":"authentication","stage":"collect","api_codes":[1,2,3,4,5,6,7,8,9]}`,
		`{"error":"authentication","stage":"collect","api_codes":["secret"]}`,
		`{"error":"authentication","stage":"collect","message":"secret"}`,
		`{"error":"authentication","stage":"secret"}`,
		`{"error":"authentication","stage":"collect","error_name":"TypeError"}`,
		`{"error":"collector_failed","stage":"collect","error_name":"secret"}`,
		`{"error":"collector_failed","stage":"collect","error_name":"TypeError","error_code":"secret"}`,
		`{"error":"secret","stage":"collect"}`,
	} {
		_, err := decodeCollectionOutput([]byte(raw), nil)
		failure, ok := errors.AsType[*CollectionError](err)

		if !ok || failure == nil {
			t.Fatalf("expected CollectionError, got %T", err)
		}

		require.Contains(t, []string{"invalid_response", "collector_failed"}, failure.Code, raw)
		require.NotContains(t, failure.Error(), "secret", raw)
	}
}

// 2026-09-24 요청 ID 초기화 장애는 모든 예외가 같은 collector_failed로 보여 로그만으로 단계를 알 수 없었다.
func TestUnclassifiedHelperFailureKeepsStageAndErrorKind(t *testing.T) {
	_, err := decodeCollectionOutput([]byte(`{"error":"collector_failed","stage":"app_shell","error_name":"TypeError","error_code":"ENOTFOUND","cooldown_seconds":0,"http_status":0,"api_codes":[]}`), errors.New("exit status 1"))
	failure, ok := errors.AsType[*CollectionError](err)

	if !ok || failure == nil {
		t.Fatalf("expected CollectionError, got %T", err)
	}

	require.Equal(t, "collector_failed", failure.Code)
	require.Contains(t, failure.Error(), "stage=app_shell error_name=TypeError error_code=ENOTFOUND ")

	code, cooldown := collectionFailure(err)
	require.Equal(t, "collector_failed", code)
	require.Zero(t, cooldown)
}

func TestUnreadableHelperOutputKeepsProcessResult(t *testing.T) {
	exitErr := exec.CommandContext(t.Context(), "sh", "-c", "exit 3").Run()
	require.Error(t, exitErr)

	for _, output := range []string{``, `not json`, `{"spaces":[]}`} {
		_, err := decodeCollectionOutput([]byte(output), exitErr)
		failure, ok := errors.AsType[*CollectionError](err)

		if !ok || failure == nil {
			t.Fatalf("expected CollectionError, got %T", err)
		}

		require.Equal(t, "collector_failed", failure.Code)
		require.Equal(t, "helper_output", failure.Stage)
		require.Contains(t, failure.Error(), `process="exit status 3"`)
	}
}

func TestCollectionOutputPreservesFailureAndEmptySuccess(t *testing.T) {
	spaces, err := decodeCollectionOutput([]byte(`{"spaces":[]}`), nil)
	require.NoError(t, err)
	require.Empty(t, spaces)

	_, err = decodeCollectionOutput([]byte(`{"spaces":[]}`), errors.New("process failed"))
	require.Error(t, err)

	_, err = decodeCollectionOutput([]byte(`{}`), nil)
	require.Error(t, err)
}

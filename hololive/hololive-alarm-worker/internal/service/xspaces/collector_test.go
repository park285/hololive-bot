package xspaces

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

func TestCollectionOutputDiagnosticBoundary(t *testing.T) {
	_, err := decodeCollectionOutput([]byte(`{"error":"authentication","stage":"collect","http_status":403,"api_codes":[32]}`))
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
		_, err := decodeCollectionOutput([]byte(raw))
		failure, ok := errors.AsType[*CollectionError](err)

		if !ok || failure == nil {
			t.Fatalf("expected CollectionError, got %T", err)
		}

		require.Contains(t, []string{"invalid_response", "collector_failed"}, failure.Code, raw)
		require.NotContains(t, failure.Error(), "secret", raw)
	}
}

func TestUnclassifiedHelperFailureKeepsStageAndErrorKind(t *testing.T) {
	_, err := decodeCollectionOutput([]byte(`{"error":"collector_failed","stage":"app_shell","error_name":"TypeError","error_code":"ENOTFOUND","cooldown_seconds":0,"http_status":0,"api_codes":[]}`))
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

func TestCollectionOutputPreservesFailureAndEmptySuccess(t *testing.T) {
	spaces, err := decodeCollectionOutput([]byte(`{"spaces":[]}`))
	require.NoError(t, err)
	require.Empty(t, spaces)

	_, err = decodeCollectionOutput([]byte(`{}`))
	require.Error(t, err)
}

const fakeHelperScript = `mode=$(cat mode 2>/dev/null)
rm -f mode
case "$mode" in
garbage) read -r line; echo 'not json'; while read -r line; do :; done ;;
exit) read -r line; exit 3 ;;
hang) while read -r line; do :; done ;;
esac
while IFS= read -r line; do
  printf '{"spaces":[{"space_id":"%s","creator_id":"1","title":"t","started_at":"2026-01-01T00:00:00Z"}]}\n' "$$"
done`

func fakeHelperCollector(t *testing.T, firstMode string) *ProcessCollector {
	t.Helper()

	dir := t.TempDir()

	if firstMode != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "mode"), []byte(firstMode), 0o600))
	}

	collector := &ProcessCollector{command: func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", "-c", fakeHelperScript)

		cmd.Dir = dir

		return cmd
	}}

	t.Cleanup(func() {
		if collector.helper != nil {
			require.ErrorContains(t, collector.helper.stop(), "signal: killed")
		}
	})

	return collector
}

func collectHelperPID(t *testing.T, collector *ProcessCollector) string {
	t.Helper()

	cookies := sessions.Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}
	spaces, err := collector.Collect(t.Context(), cookies, []string{"1"})
	require.NoError(t, err)
	require.Len(t, spaces, 1)

	return spaces[0].SpaceID
}

func TestProcessCollectorReusesResidentHelper(t *testing.T) {
	collector := fakeHelperCollector(t, "")

	first := collectHelperPID(t, collector)
	require.Equal(t, first, collectHelperPID(t, collector))
	require.Equal(t, first, collectHelperPID(t, collector))
}

func TestProcessCollectorReplacesHelperThatExitedWhileIdle(t *testing.T) {
	collector := fakeHelperCollector(t, "")

	first := collectHelperPID(t, collector)

	require.NoError(t, collector.helper.cmd.Process.Kill())
	require.Eventually(t, collector.helper.hasExited, 5*time.Second, 10*time.Millisecond)

	replacement := collectHelperPID(t, collector)
	require.NotEqual(t, first, replacement)
	require.Equal(t, replacement, collectHelperPID(t, collector))
}

func TestProcessCollectorReplacesFailedHelper(t *testing.T) {
	cookies := sessions.Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}

	for _, tc := range []struct {
		mode    string
		code    string
		process string
		timeout time.Duration
	}{
		{mode: "garbage", code: "collector_failed", process: "signal: killed", timeout: time.Minute},
		{mode: "exit", code: "collector_failed", process: "exit status 3", timeout: time.Minute},
		{mode: "hang", code: "timeout", timeout: 100 * time.Millisecond},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			collector := fakeHelperCollector(t, tc.mode)

			ctx, cancel := context.WithTimeout(t.Context(), tc.timeout)

			defer cancel()

			_, err := collector.Collect(ctx, cookies, []string{"1"})
			failure, ok := errors.AsType[*CollectionError](err)
			require.True(t, ok, "expected CollectionError, got %T", err)
			require.Equal(t, tc.code, failure.Code)
			require.Contains(t, failure.Process, tc.process)
			require.Nil(t, collector.helper)

			replacement := collectHelperPID(t, collector)
			require.Equal(t, replacement, collectHelperPID(t, collector))
		})
	}
}

package runtimescripts

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestSetIrisBaseURLWritesOwnerOnlyFileWithoutLeaking(t *testing.T) {
	t.Parallel()

	const secretURL = "https://user:secret@iris.internal.example/path?token=abc" // #nosec G101 -- 출력 유출을 확인하려고 만든 가짜 자격 증명이다.

	dir := filepath.Join(t.TempDir(), "runtime-config")
	r := harness.Bash(t, harness.Cmd{
		Script: `"$SCRIPTTEST_ROOT/scripts/runtime/set-iris-base-url.sh" "$1"`,
		Args:   []string{secretURL},
		Env:    []string{"RUNTIME_CONFIG_DIR=" + dir, "IRIS_TRANSPORT=h3"},
	})
	harness.Succeeds(t, "set-iris-base-url", r)

	for _, leaked := range []string{secretURL, "user:secret", "token=abc"} {
		if strings.Contains(r.Stdout, leaked) {
			t.Errorf("stdout leaked %q: %s", leaked, r.Stdout)
		}
	}

	target := filepath.Join(dir, "iris_base_url")
	if mode := harness.Mode(t, target).Perm(); mode != 0o600 && mode != 0o400 {
		t.Errorf("iris_base_url mode = %o, want 600 or 400", mode)
	}

	if got := strings.TrimSuffix(harness.Read(t, target), "\n"); got != secretURL {
		t.Errorf("iris_base_url = %q, want the supplied URL", got)
	}
}

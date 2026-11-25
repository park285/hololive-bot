package logscripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestStreamMirrorRotatesAndPrunes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := filepath.Join(dir, "mirror", "bot.log")
	chunk := filepath.Join(dir, "chunk")

	appendChunk := func(label string) {
		t.Helper()

		harness.Write(t, chunk, label+"\n")
		harness.Succeeds(t, "append "+label, harness.Bash(t, harness.Cmd{
			Script: `source "$SCRIPTTEST_ROOT/scripts/logs/lib/stream.sh"; stream_mirror_append_file "$1" "$2"`,
			Args:   []string{log, chunk},
			Env:    []string{"STREAM_MIRROR_MAX_BYTES=20", "STREAM_MIRROR_MAX_ROTATIONS=2"},
		}))
	}
	expect := func(step string, want map[string]string) {
		t.Helper()

		for suffix, content := range want {
			if got := harness.Read(t, log+suffix); got != content+"\n" {
				t.Errorf("after %s, bot.log%s = %q, want %q", step, suffix, got, content)
			}
		}
	}

	harness.Write(t, log, "")
	harness.Remove(t, log)
	appendChunk("first-entry")
	expect("first chunk", map[string]string{"": "first-entry"})
	appendChunk("second-entry")
	expect("second chunk", map[string]string{"": "second-entry", ".1": "first-entry"})
	appendChunk("third-entry")
	expect("third chunk", map[string]string{"": "third-entry", ".1": "second-entry", ".2": "first-entry"})
	appendChunk("fourth-entry")
	expect("fourth chunk", map[string]string{".2": "second-entry"})

	if _, err := os.Lstat(log + ".3"); err == nil {
		t.Error("rotation kept bot.log.3 beyond the retention limit")
	}
}

func TestStreamWorkerBoundsLiveMirror(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	repo := filepath.Join(dir, "worker-repo")
	fakeLogs := filepath.Join(dir, "fake-logs.sh")
	harness.Write(t, filepath.Join(repo, "logs", "mirror", ".keep"), "")
	harness.Executable(t, fakeLogs, `set -euo pipefail
[[ "${1:-}" == tail ]] || exit 2
dd if=/dev/zero bs=256 count=8 status=none | tr '\0' 'x'
sleep 5
`)

	r := harness.Bash(t, harness.Cmd{
		Script: `source "$SCRIPTTEST_ROOT/scripts/logs/lib/stream.sh"
(run_stream_service_worker hololive-api) >/dev/null 2>&1 &
worker=$!
sleep 1
find "$REPO_ROOT/logs/mirror" -type f -size +64c -print
pkill -TERM -P "$worker" 2>/dev/null || true
kill "$worker" 2>/dev/null || true
wait "$worker" 2>/dev/null || true`,
		Env: []string{"REPO_ROOT=" + repo, "SCRIPT_PATH=" + fakeLogs, "STREAM_MIRROR_MAX_BYTES=64", "STREAM_MIRROR_MAX_ROTATIONS=1"},
	})
	harness.Succeeds(t, "stream worker", r)

	if oversized := strings.TrimSpace(r.Stdout); oversized != "" {
		t.Errorf("stream worker left oversized mirror files: %s", oversized)
	}

	mirror := filepath.Join(repo, "logs", "mirror", "hololive-api.log")
	if harness.Size(t, mirror) == 0 && harness.Size(t, mirror+".1") == 0 {
		t.Error("stream worker did not append live output")
	}
}

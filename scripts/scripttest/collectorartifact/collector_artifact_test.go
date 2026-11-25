package collectorartifact

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const checkArtifact = `exec sh "$SCRIPTTEST_ROOT/scripts/build/check-hololive-collector-go-artifact.sh" "$1" --version "$2" --revision "$3" --goos linux --goarch amd64 --goamd64 v1`

func gitHead(t *testing.T, args ...string) string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: `git -C "$SCRIPTTEST_ROOT" rev-parse "$@"`, Args: args})
	harness.Succeeds(t, "git rev-parse", r)

	return strings.TrimSpace(r.Stdout)
}

func TestCollectorBuildRejectsUnsafeInputs(t *testing.T) {
	t.Parallel()

	revision := gitHead(t, "--verify", "HEAD^{commit}")
	cases := map[string][2]string{
		"invalid revision":                {"test", "not-a-revision"},
		"unknown revision without opt-in": {"test", "unknown"},
		"unsafe version":                  {"bad version", revision},
	}

	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, harness.Cmd{
				Script: `exec sh "$SCRIPTTEST_ROOT/scripts/build/build-hololive-collector-go.sh" --output-dir "$1" --version "$2" --revision "$3" --goos linux --goarch amd64 --goamd64 v1`,
				Args:   []string{filepath.Join(t.TempDir(), "candidate"), input[0], input[1]},
			})
			if r.ExitCode != 2 {
				t.Errorf("%s: exit = %d, want 2: %s", name, r.ExitCode, r)
			}
		})
	}
}

func TestCollectorArtifactCheckRejectsAlteredArtifacts(t *testing.T) {
	t.Parallel()

	revision := gitHead(t, "--verify", "HEAD^{commit}")
	version := gitHead(t, "--short=12", "HEAD")
	runnerTemp := t.TempDir()
	harness.Succeeds(t, "collector build-prod", harness.Bash(t, harness.Cmd{
		Script: `exec bash "$SCRIPTTEST_ROOT/scripts/ci/public-pr-go-gate.sh" hololive/hololive-collector build-prod`,
		Env:    []string{"HOLO_BOT_VERSION=" + version, "RUNNER_TEMP=" + runnerTemp},
	}))

	built := filepath.Join(runnerTemp, "collector-build-prod")

	t.Run("artifact from a different release", func(t *testing.T) {
		t.Parallel()

		r := harness.Bash(t, harness.Cmd{Script: checkArtifact, Args: []string{built, version + "-mismatch", revision}})
		if r.ExitCode != 1 {
			t.Errorf("exit = %d, want 1: %s", r.ExitCode, r)
		}
	})

	cases := map[string]string{
		"manifest identity mismatch":        `jq '.source_revision = ("0" * 40)' "$1/manifest.json" >"$2/manifest.json"`,
		"incorrect manifest version":        `jq --arg wrong "$3-mismatch" '.version = $wrong' "$1/manifest.json" >"$2/manifest.json"`,
		"incorrect binary hash in manifest": `jq '.files["bin/hololive-collector"] = ("0" * 64)' "$1/manifest.json" >"$2/manifest.json"`,
		"incorrect checksum file":           `printf '%064d  %s\n' 0 bin/hololive-collector >"$2/sha256sums.txt"`,
		"collector substituted for healthcheck with matching checksums": `cp "$2/bin/hololive-collector" "$2/bin/healthcheck"
jq '.files["bin/healthcheck"] = .files["bin/hololive-collector"]' "$1/manifest.json" >"$2/manifest.json"
(cd "$2" && sha256sum bin/hololive-collector bin/healthcheck >sha256sums.txt)`,
	}
	for name, alter := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := t.TempDir()
			harness.Succeeds(t, "prepare "+name, harness.Bash(t, harness.Cmd{
				Script: `set -euo pipefail
mkdir -p "$2/bin"
cp -L "$1/bin/hololive-collector" "$1/bin/healthcheck" "$2/bin/"
cp -L "$1/sha256sums.txt" "$1/manifest.json" "$2/"
` + alter,
				Args: []string{built, fixture, version},
			}))

			r := harness.Bash(t, harness.Cmd{Script: checkArtifact, Args: []string{fixture, version, revision}})
			if r.ExitCode != 1 {
				t.Errorf("exit = %d, want 1: %s", r.ExitCode, r)
			}

			if strings.Contains(name, "healthcheck") && !strings.Contains(r.Stdout+r.Stderr, "bin/healthcheck package") {
				t.Errorf("substituted healthcheck was not identified: %s", r)
			}
		})
	}
}

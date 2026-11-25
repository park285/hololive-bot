package imagepermissions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const walker = `
const fs = require("fs"), path = require("path");
const { R_OK, X_OK } = fs.constants;
const denied = [];
let files = 0;
(function walk(p) {
  const st = fs.lstatSync(p);
  if (st.isSymbolicLink()) return;
  if (st.isDirectory()) {
    try { fs.accessSync(p, R_OK | X_OK); } catch { denied.push(p + "/"); return; }
    for (const name of fs.readdirSync(p)) walk(path.join(p, name));
    return;
  }
  files++;
  try { fs.accessSync(p, R_OK); } catch { denied.push(p); }
})(process.argv[1]);
if (files === 0) { console.error("no files under " + process.argv[1]); process.exit(1); }
if (denied.length > 0) { console.error(denied.slice(0, 20).join("\n")); process.exit(1); }
`

const (
	collector = "hololive/hololive-collector"
	alarm     = "hololive/hololive-alarm-worker"
)

func umask077Checkout(t *testing.T) string {
	t.Helper()

	checkout := filepath.Join(t.TempDir(), "checkout")
	paths := []string{
		collector + "/Dockerfile.po-sandbox", collector + "/Dockerfile.po-sandbox.dockerignore",
		collector + "/Dockerfile", collector + "/Dockerfile.dockerignore",
		alarm + "/Dockerfile", alarm + "/Dockerfile.dockerignore",
		collector + "/po-sandbox/package.json", collector + "/po-sandbox/package-lock.json", collector + "/po-sandbox/src",
		collector + "/youtubejs/package.json", collector + "/youtubejs/package-lock.json", collector + "/youtubejs/src",
		collector + "/eventgrade/server.mjs",
		alarm + "/xspaces/package.json", alarm + "/xspaces/package-lock.json", alarm + "/xspaces/src",
	}
	harness.Succeeds(t, "umask 077 checkout", harness.Bash(t, harness.Cmd{
		Script: `set -euo pipefail
checkout="$1"; shift
mkdir -p "$checkout"
git -C "$SCRIPTTEST_ROOT" ls-files -z -- "$@" | tar -C "$SCRIPTTEST_ROOT" --null -T - -cf - | (umask 077 && tar -xf - --no-same-permissions -C "$checkout")`,
		Args: append([]string{checkout}, paths...),
	}))

	if mode := harness.Mode(t, filepath.Join(checkout, collector, "po-sandbox", "src", "worker.mjs")).Perm(); mode != 0o600 {
		t.Fatalf("fixture must reproduce a umask 077 checkout, worker.mjs mode = %o", mode)
	}

	return checkout
}

func TestImageRuntimeTreesReadableAfterUmask077Checkout(t *testing.T) {
	t.Parallel()

	if os.Getenv("CONTAINER_HOST")+os.Getenv("CONTAINER_CONNECTION") != "" {
		t.Fatal("remote Podman connection configured; run this image build check against local rootless Podman on kapu")
	}

	if r := harness.Bash(t, harness.Cmd{Script: `podman info --format '{{.Host.Security.Rootless}}'`}); strings.TrimSpace(r.Stdout) != "true" {
		t.Fatalf("local rootless Podman unavailable; run this image build check on kapu: %s", r)
	}

	checkout := umask077Checkout(t)
	stages := []struct{ dockerfile, target, tree, user string }{
		{collector + "/Dockerfile.po-sandbox", "package-build", "/app/po-sandbox", "65532:1000"},
		{collector + "/Dockerfile", "youtubejs-build", "/app/youtubejs", "1000:1000"},
		{collector + "/Dockerfile", "eventgrade-build", "/app/eventgrade", "1000:1000"},
		{alarm + "/Dockerfile", "xspaces-build", "/app/xspaces", "1000:1000"},
	}

	for _, s := range stages {
		t.Run(s.dockerfile+" "+s.target, func(t *testing.T) {
			t.Parallel()

			tag := fmt.Sprintf("localhost/hololive-umask-check-%d-%s", os.Getpid(), s.target)
			harness.CleanupBash(t, harness.Cmd{Script: `podman image rm -f "$1" >/dev/null`, Args: []string{tag}})

			harness.Succeeds(t, "build", harness.Bash(t, harness.Cmd{
				Script: `exec podman build --quiet --target "$1" --tag "$2" --file "$3" "$4"`,
				Args:   []string{s.target, tag, filepath.Join(checkout, s.dockerfile), checkout},
			}))

			r := harness.Bash(t, harness.Cmd{
				Script: `exec podman run --rm --pull=never --network none --user "$1" --entrypoint /usr/local/bin/node "$2" -e "$3" "$4"`,
				Args:   []string{s.user, tag, walker, s.tree},
			})
			if r.ExitCode != 0 {
				t.Errorf("%s is not readable by runtime user %s after a umask 077 checkout: %s", s.tree, s.user, r)
			}
		})
	}
}

package prestart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const prestartPodman = `case "$1 $2" in
  "volume inspect") echo "$VOLUME_ROOT/$5" ;;
  "volume ls") ls "$VOLUME_ROOT" ;;
  *) exit 2 ;;
esac
`

type prestartHost struct {
	root, volumes string
}

func newPrestartHost(t *testing.T) prestartHost {
	t.Helper()

	dir := t.TempDir()
	h := prestartHost{root: filepath.Join(dir, "host"), volumes: filepath.Join(dir, "volumes")}

	if err := os.MkdirAll(filepath.Join(h.volumes, "hololive-bot_holo-pg-data19"), 0o700); err != nil {
		t.Fatal(err)
	}

	copyInto(t, "deploy/quadlet/hololive-bot/holo-pg-data.volume", filepath.Join(h.root, "units/holo-pg-data.volume"))
	harness.Executable(t, filepath.Join(h.root, "bin/podman"), prestartPodman)
	harness.Write(t, filepath.Join(h.root, "compose.env"), "")

	if err := os.MkdirAll(filepath.Join(h.root, "var/lib/hololive-bot"), 0o700); err != nil {
		t.Fatal(err)
	}

	return h
}

func (h prestartHost) output(t *testing.T) string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{
		Script: `bash "$SCRIPTTEST_ROOT/scripts/deploy/quadlet-prestart.sh" holo-postgres || true`,
		Env:    []string{"HOLOLIVE_QUADLET_TEST_ROOT=" + h.root, "VOLUME_ROOT=" + h.volumes},
		Bin:    filepath.Join(h.root, "bin"),
	})

	return r.Stdout + r.Stderr
}

func (h prestartHost) expect(t *testing.T, label, want string) {
	t.Helper()

	if got := h.output(t); !strings.Contains(got, want) {
		t.Fatalf("%s: prestart output missing %q:\n%s", label, want, got)
	}
}

const composeCheckReached = "compose env file is invalid"

func TestPrestartRefusesAnEmptyClusterBesideAPreviousOne(t *testing.T) {
	t.Parallel()

	h := newPrestartHost(t)
	h.expect(t, "fresh cluster", composeCheckReached)

	if err := os.MkdirAll(filepath.Join(h.volumes, "hololive-bot_holo-pg-data"), 0o700); err != nil {
		t.Fatal(err)
	}

	h.expect(t, "empty cluster beside previous", "holds a previous one")
	harness.Write(t, filepath.Join(h.volumes, "hololive-bot_holo-pg-data19/pgdata/PG_VERSION"), "")
	h.expect(t, "initialized cluster", composeCheckReached)
}

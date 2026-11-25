package logscripts

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type rollupFixture struct {
	tmp, logs, state string
}

func newRollupFixture(t *testing.T) rollupFixture {
	t.Helper()

	tmp := t.TempDir()
	f := rollupFixture{tmp: tmp, logs: filepath.Join(tmp, "logs"), state: filepath.Join(tmp, "state")}
	harness.Write(t, filepath.Join(f.logs, ".keep"), "")
	harness.Write(t, filepath.Join(f.state, ".keep"), "")

	return f
}

func (f rollupFixture) source(t *testing.T, body string, args ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `. "$SCRIPTTEST_ROOT/scripts/logs/daily-rollup-logs.sh"
` + body,
		Args: args,
		Env:  []string{"LOG_ROOT=" + f.logs, "LOG_ROLLUP_STATE_DIR=" + f.state},
	})
}

func (f rollupFixture) once(t *testing.T, env ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `exec "$SCRIPTTEST_ROOT/scripts/logs/daily-rollup-logs.sh" once`,
		Env:    append([]string{"LOG_ROOT=" + f.logs, "LOG_ROLLUP_STATE_DIR=" + f.state}, env...),
	})
}

func requireContent(t *testing.T, path, want string) {
	t.Helper()

	if got := harness.Read(t, path); got != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

func requireRegular(t *testing.T, path string) {
	t.Helper()

	if mode := harness.Mode(t, path); !mode.IsRegular() {
		t.Errorf("%s mode = %v, want a regular file", path, mode)
	}
}

func TestSnapshotRefusesSymlinkedLog(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	secret := filepath.Join(f.tmp, "secret")
	harness.Write(t, secret, "SECRET")

	logPath := filepath.Join(f.logs, "svc.log")
	harness.Symlink(t, secret, logPath)

	r := f.source(t, `snapshot_and_truncate_log "$1" "$(mktemp -d "$LOG_ROLLUP_STATE_DIR/.rollup.XXXXXX")/svc.log"`, logPath)
	harness.Fails(t, "snapshot of a symlinked log", r)
	requireContent(t, secret, "SECRET")

	if mode := harness.Mode(t, secret).Perm(); mode != 0o600 {
		t.Errorf("symlink target mode changed to %o", mode)
	}

	if harness.Mode(t, logPath)&os.ModeSymlink == 0 {
		t.Error("planted symlink was replaced")
	}
}

func TestSnapshotKeepsOpenWritersOnVisibleLog(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	logPath := filepath.Join(f.logs, "ok.log")
	snapshot := filepath.Join(f.state, "snapshot", "ok.log")

	harness.Write(t, logPath, "before-rollup\n")
	harness.Write(t, filepath.Join(filepath.Dir(snapshot), ".keep"), "")

	harness.Succeeds(t, "snapshot with an open writer", f.source(t, `exec {held}>>"$1"
printf 'held-before\n' >&"$held"
snapshot_and_truncate_log "$1" "$2"
printf 'held-after\n' >&"$held"`, logPath, snapshot))

	if !strings.Contains(harness.Read(t, snapshot), "held-before") {
		t.Error("snapshot lost the write made before truncation")
	}

	if !strings.Contains(harness.Read(t, logPath), "held-after") {
		t.Error("open writer did not keep writing to the visible log")
	}
}

func TestSnapshotRefusesMultiLinkInode(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	logPath := filepath.Join(f.logs, "hardlink.log")
	alias := filepath.Join(f.tmp, "hardlink-alias.log")

	harness.Write(t, logPath, "hardlink-content\n")

	if err := os.Link(logPath, alias); err != nil {
		t.Fatalf("link: %v", err)
	}

	r := f.source(t, `snapshot_and_truncate_log "$1" "$(mktemp -d "$LOG_ROLLUP_STATE_DIR/.rollup.XXXXXX")/hardlink.log"`, logPath)
	if r.ExitCode != 77 {
		t.Errorf("multi-link snapshot exit = %d, want 77: %s", r.ExitCode, r)
	}

	requireContent(t, logPath, "hardlink-content\n")
	requireContent(t, alias, "hardlink-content\n")
}

func TestRollupIgnoresPlantedSymlinks(t *testing.T) {
	t.Parallel()

	t.Run("legacy log-root lock", func(t *testing.T) {
		t.Parallel()

		f := newRollupFixture(t)
		target := filepath.Join(f.tmp, "lock-secret")
		harness.Write(t, target, "LOCKSECRET")
		harness.Symlink(t, target, filepath.Join(f.logs, ".daily-rollup.lock"))

		harness.Succeeds(t, "rollup with a legacy lock symlink", f.source(t, "rollup_once"))
		requireContent(t, target, "LOCKSECRET")
	})

	t.Run("state lock dir", func(t *testing.T) {
		t.Parallel()

		f := newRollupFixture(t)
		target := filepath.Join(f.tmp, "lock-dir-target")
		planted := filepath.Join(f.tmp, "planted-lock-dir")
		harness.Write(t, filepath.Join(target, ".keep"), "")
		harness.Symlink(t, target, planted)

		harness.Fails(t, "rollup with a symlinked lock dir", f.source(t, `LOCK_DIR="$1"; rollup_once`, planted))

		if !harness.Mode(t, target).IsDir() {
			t.Error("state lock target was modified")
		}
	})

	t.Run("pre-existing archive", func(t *testing.T) {
		t.Parallel()

		f := newRollupFixture(t)
		secret := filepath.Join(f.tmp, "archive-secret")
		harness.Write(t, secret, "ARCHIVESECRET")
		harness.Write(t, filepath.Join(f.logs, "evil.log"), "archive me\n")
		harness.Write(t, filepath.Join(f.logs, "archive", ".keep"), "")
		harness.Symlink(t, secret, filepath.Join(f.logs, "archive", "evil-2026-06-20.log.tar.gz"))

		harness.Succeeds(t, "rollup over an archive symlink", f.source(t, "ROLLUP_DATE=2026-06-20; rollup_once"))
		requireContent(t, secret, "ARCHIVESECRET")
	})
}

func TestRollupStagesOutsideLogRoot(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	harness.Write(t, filepath.Join(f.logs, "safe.log"), "safe log\n")
	harness.Succeeds(t, "safe rollup", f.source(t, "ROLLUP_DATE=2026-06-20; rollup_once"))

	staged, err := filepath.Glob(filepath.Join(f.logs, ".daily-rollup-*"))
	if err != nil || len(staged) != 0 {
		t.Errorf("rollup staging leaked into LOG_ROOT: %q %v", staged, err)
	}

	requireRegular(t, filepath.Join(f.logs, "archive", "safe-2026-06-20.log.tar.gz"))
}

func archiveMember(t *testing.T, archive, name string) string {
	t.Helper()

	file, err := os.Open(archive) //nolint:gosec // 테스트가 만든 archive만 연다
	if err != nil {
		t.Fatalf("open %s: %v", archive, err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip %s: %v", archive, err)
	}

	tr := tar.NewReader(gz)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("%s has no member %s", archive, name)
		}

		if err != nil {
			t.Fatalf("read %s: %v", archive, err)
		}

		if header.Name == name {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s from %s: %v", name, archive, err)
			}

			return string(data)
		}
	}
}

func TestOnceModeArchivesTruncatesAndSkipsMirrors(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	mirror := filepath.Join(f.logs, "remote", "osaka", "collector-a.log")
	harness.Write(t, filepath.Join(f.logs, "bot.log"), "bot yesterday\n")
	harness.Write(t, mirror, "collector mirror\n")
	harness.Symlink(t, "remote/osaka/collector-a.log", filepath.Join(f.logs, "collector-a.log"))

	archive := filepath.Join(f.logs, "archive", "bot-2026-05-20.log.tar.gz")

	for _, run := range []string{"first run", "idempotent rerun"} {
		harness.Succeeds(t, run, f.once(t, "LOG_ROLLUP_DATE=2026-05-20"))
		requireRegular(t, archive)
	}

	requireContent(t, filepath.Join(f.logs, "bot.log"), "")
	requireContent(t, mirror, "collector mirror\n")

	if got := archiveMember(t, archive, "bot.log"); got != "bot yesterday\n" {
		t.Errorf("archived bot.log = %q, want the pre-rollup content", got)
	}
}

func TestOnceModeRestrictsToSelectedFiles(t *testing.T) {
	t.Parallel()

	f := newRollupFixture(t)
	harness.Write(t, filepath.Join(f.logs, "producer.log"), "producer regular\n")
	harness.Write(t, filepath.Join(f.logs, "legacy.log"), "legacy regular\n")

	harness.Succeeds(t, "filtered rollup", f.once(t, "LOG_ROLLUP_DATE=2026-05-21", "LOG_ROLLUP_FILES=producer.log"))
	requireRegular(t, filepath.Join(f.logs, "archive", "producer-2026-05-21.log.tar.gz"))
	requireContent(t, filepath.Join(f.logs, "legacy.log"), "legacy regular\n")
}

func TestOnceModeMetrics(t *testing.T) {
	t.Parallel()

	t.Run("emits atomic world-readable last-success metric", func(t *testing.T) {
		t.Parallel()

		f := newRollupFixture(t)
		metrics := filepath.Join(f.tmp, "textfile")
		harness.Write(t, filepath.Join(f.logs, "svc.log"), "metric log\n")
		harness.Write(t, filepath.Join(metrics, ".keep"), "")

		harness.Succeeds(t, "metric rollup", f.once(t, "LOG_ROLLUP_DATE=2026-05-22", "LOG_ROLLUP_METRICS_DIR="+metrics))

		prom := filepath.Join(metrics, "hololive_log_rollup.prom")
		if got := harness.Read(t, prom); !regexp.MustCompile(`(?m)^hololive_log_rollup_last_success_timestamp_seconds [0-9]+$`).MatchString(got) {
			t.Errorf("metric file = %q, want a last-success timestamp", got)
		}

		if mode := harness.Mode(t, prom).Perm(); mode != 0o644 {
			t.Errorf("metric file mode = %o, want 644", mode)
		}

		if staged, err := filepath.Glob(filepath.Join(metrics, ".hololive_log_rollup.*")); err != nil || len(staged) != 0 {
			t.Errorf("metric staging leaked: %q %v", staged, err)
		}
	})

	t.Run("absent metrics dir is a no-op", func(t *testing.T) {
		t.Parallel()

		f := newRollupFixture(t)
		absent := filepath.Join(f.tmp, "absent")
		harness.Write(t, filepath.Join(f.logs, "svc.log"), "noop log\n")

		harness.Succeeds(t, "rollup with absent metrics dir", f.once(t, "LOG_ROLLUP_DATE=2026-05-23", "LOG_ROLLUP_METRICS_DIR="+absent))

		if _, err := os.Lstat(absent); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("absent metrics dir was created: %v", err)
		}
	})
}

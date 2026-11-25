package syncopt

import (
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestSyncOptRejectsUnitsThatDoNotGenerate(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	base := e.repo(t, "repo")

	broken := e.clone(t, base, "repo-broken-unit")
	put(t, filepath.Join(broken, "deploy/quadlet/hololive-bot/broken.container"), "[Container]\nContainerName=broken\n")
	commit(t, broken, "broken-unit")
	e.rejects(t, e.host(t, "host-broken-unit", "1"), broken, "broken staged unit", unitsDoNotGenerate)

	dropIn := e.clone(t, base, "repo-broken-dropin")
	appendFile(t, filepath.Join(dropIn, "deploy/quadlet/optional/x-spaces/hololive-api.container.d/40-x-spaces.conf"), "[Container]\nUnknownKeyForSyncTest=1\n")
	commit(t, dropIn, "broken-dropin")
	e.rejects(t, e.host(t, "host-broken-dropin", "1"), dropIn, "broken x-spaces drop-in", unitsDoNotGenerate)

	h := e.host(t, "host-dropin-disabled", "0")
	e.syncs(t, h, dropIn, "disabled x-spaces drop-in")

	for _, unit := range []string{"hololive-api", "hololive-alarm-worker"} {
		if exists(h.installed(unit + ".container.d/40-x-spaces.conf")) {
			t.Errorf("x-spaces drop-in stayed installed for %s with HOLOLIVE_X_SPACES_ENABLED=0", unit)
		}
	}
}

func TestSyncOptRejectsAnExhaustedConnectionBudget(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")
	h := e.host(t, "host", "0")
	appendFile(t, h.compose(), "BOT_POSTGRES_POOL_MAX_CONNS=40\n")
	e.rejects(t, h, repo, "connection budget", "PostgreSQL connection budget check failed")
}

func TestSyncOptRejectsUnsafeComposeValues(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")

	for i, line := range []string{"SPACE= value", "SPACE=value ", "COMMAND=$(id)", "COMMAND=`id`", "CONTROL=a\x01b"} {
		h := e.host(t, "host-invalid-env-"+itoa(uint64(i)), "1") // #nosec G115 -- 작은 양수 인덱스다.
		appendFile(t, h.compose(), line+"\n")
		e.rejects(t, h, repo, "invalid compose env "+line, "[ERROR]")
	}
}

var runtimePaths = []string{"data", "logs", "backups", "runtime-config", "artifacts/worker-profile"}

func runtimeSnapshot(t *testing.T, h syncHost) string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{
		Script: `cd "$1/opt/current" && find data logs backups runtime-config artifacts/worker-profile \
  -printf '%p %m %U:%G\n' -type f -exec sha256sum {} + | LC_ALL=C sort`,
		Args: []string{string(h)},
	})
	harness.Succeeds(t, "runtime snapshot", r)

	return r.Stdout
}

func writableExecutionPath(t *testing.T, root string) string {
	t.Helper()

	var found string

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatalf("walk execution tree: %v", err)
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("relative execution path: %v", relErr)
		}

		if d.IsDir() && slices.Contains(runtimePaths, rel) {
			return filepath.SkipDir
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			t.Fatalf("stat execution path: %v", infoErr)
		}

		if info.Mode().Perm()&0o022 == 0 {
			return nil
		}

		found = rel

		return fs.SkipAll
	})
	if walkErr != nil {
		t.Fatalf("walk execution tree: %v", walkErr)
	}

	return found
}

func unreviewedRuntimeRepo(t *testing.T, e syncEnv, name string) string {
	t.Helper()

	repo := e.repo(t, name)
	put(t, filepath.Join(repo, "runtime-config/unreviewed"), "unreviewed\n")

	return repo
}

func TestSyncOptPreserveRuntimeKeepsRuntimeBytes(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := unreviewedRuntimeRepo(t, e, "repo")
	h := e.host(t, "host", "0")

	for _, path := range runtimePaths {
		retained := h.at("opt/current/" + path + "/retained")
		put(t, retained, "runtime-owned\n")
		chmod(t, retained, 0o660)
	}

	before := runtimeSnapshot(t, h)
	e.syncs(t, h, repo, "runtime-preserving sync", "--preserve-runtime")

	if runtimeSnapshot(t, h) != before {
		t.Fatal("artifact-only sync changed runtime bytes, ownership or mode")
	}

	if strings.TrimSpace(harness.Read(t, h.at("opt/current/.source-revision"))) != head(t, repo) {
		t.Fatal("artifact-only sync did not publish the staged tree")
	}

	if writable := writableExecutionPath(t, h.at("opt/current")); writable != "" {
		t.Fatalf("artifact-only sync installed group- or other-writable execution files: %s", writable)
	}

	for _, backup := range backups(t, h, "") {
		if exists(filepath.Join(backup, "runtime-config")) {
			t.Fatal("artifact-only backup copied runtime configuration")
		}
	}
}

func TestSyncOptRetainsEveryEarlierRecoveryPoint(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")
	h := e.host(t, "host", "0")

	for _, n := range []string{"1", "2", "3", "4"} {
		put(t, h.at("opt/current.bak-old-"+n+"/marker"), "retained-"+n+"\n")
	}

	e.syncs(t, h, repo, "first retained-backup sync")

	initial := ""

	for _, backup := range backups(t, h, "") {
		if data, err := os.ReadFile(filepath.Join(backup, "marker")); err == nil && string(data) == "old current\n" { // #nosec G304 -- fixture 경로만 읽는다.
			initial = backup
		}
	}

	if initial == "" {
		t.Fatal("first sync did not retain the original execution tree")
	}

	e.syncs(t, h, repo, "repeated retained-backup sync")

	if harness.Read(t, filepath.Join(initial, "marker")) != "old current\n" {
		t.Fatal("same-revision sync replaced the original recovery point")
	}

	for _, n := range []string{"1", "2", "3", "4"} {
		if harness.Read(t, h.at("opt/current.bak-old-"+n+"/marker")) != "retained-"+n+"\n" {
			t.Fatalf("repeated sync changed backup old-%s", n)
		}
	}

	source := ""

	for _, backup := range backups(t, h, "") {
		if exists(filepath.Join(backup, ".source-revision")) {
			source = backup
		}
	}

	if source == "" || strings.TrimSpace(harness.Read(t, filepath.Join(source, ".source-revision"))) != head(t, repo) {
		t.Fatal("repeated sync did not retain the previously deployed source")
	}
}

const collectorDeploy = `set -euo pipefail
mount --bind "$1/run-lock" /run/lock
{ . "$2/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble c; cat "$2/scripts/deploy/lib/po-central-remote.sh"; } |
  bash -s -- c deploy \
  /opt/hololive-bot/compose/po-staging-20261118T000000Z \
  /opt/hololive-bot/compose/backups/card-po-c-20261118T000000Z \
  0123456789abcdef0123456789abcdef01234567 1.0.0 -
`

func TestSyncOptAndCollectorCRejectAHeldCentralWriterLock(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")
	h := e.host(t, "host", "0")

	lock, err := os.OpenFile(h.at("run-lock/hololive-quadlet-redeploy.lock"), os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G302 G304 -- 잠금 파일 fixture다.
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := lock.Close(); err != nil {
			t.Error(err)
		}
	}()

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil { // #nosec G115 -- 열린 파일 기술자다.
		t.Fatal(err)
	}

	e.rejects(t, h, repo, "central writer lock", "another central Quadlet writer is running")
	e.rejects(t, h, repo, "card writer lock", "another central Quadlet writer is running", "--application-only")

	r := harness.Bash(t, harness.Cmd{
		Script: `unshare --user --map-root-user --mount bash -c "$COLLECTOR_DEPLOY" _ "$1" "$SCRIPTTEST_ROOT"`,
		Args:   []string{string(h)},
		Env:    []string{"COLLECTOR_DEPLOY=" + collectorDeploy},
	})
	harness.Fails(t, "collector c with a held central writer lock", r)

	if !strings.Contains(r.Stdout+r.Stderr, "another Quadlet writer is running") {
		t.Fatalf("collector c did not reject the central writer lock:\n%s", r)
	}
}

func userWriteACL(t *testing.T, path string) {
	t.Helper()

	entries := []struct {
		tag, perm uint16
		id        uint32
	}{{0x01, 7, 0xFFFFFFFF}, {0x02, 6, uint32(os.Getuid())}, {0x04, 5, 0xFFFFFFFF}, {0x10, 5, 0xFFFFFFFF}, {0x20, 5, 0xFFFFFFFF}} // #nosec G115 -- uid는 uint32 범위다.
	acl := binary.LittleEndian.AppendUint32(make([]byte, 0, 4+8*len(entries)), 2)

	for _, entry := range entries {
		acl = binary.LittleEndian.AppendUint16(acl, entry.tag)
		acl = binary.LittleEndian.AppendUint16(acl, entry.perm)
		acl = binary.LittleEndian.AppendUint32(acl, entry.id)
	}

	if err := syscall.Setxattr(path, "system.posix_acl_access", acl, 0); err != nil {
		t.Fatalf("set POSIX ACL on %s: %v", path, err)
	}
}

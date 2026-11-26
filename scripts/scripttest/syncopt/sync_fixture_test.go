package syncopt

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const quadletGenerator = "/usr/libexec/podman/quadlet"

var syncFakes = map[string]string{
	"hostname": "echo hololive-seoul\n",
	"systemctl": `printf 'systemctl %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
case "$1" in
  is-active) exit 3 ;;
  is-enabled) exit 1 ;;
  show)
    case "$*" in
      *--property=UnitPath*) echo /etc/systemd/system /run/systemd/system ;;
      *--property=NeedDaemonReload*) echo no ;;
      *--property=Names*) if [[ -e "${SYNC_TEST_EVENTS}.alias" ]]; then echo "$2 valkey-alias.service"; else echo "$2"; fi ;;
      *--property=DropInPaths*) if [[ -e "${SYNC_TEST_EVENTS}.alias-dropin" ]]; then echo /etc/systemd/system/valkey-alias.service.d/10-image.conf; fi ;;
      *--property=LoadState*)
        if [[ -e "${SYNC_TEST_EVENTS}.not-found" ]]; then echo not-found
        elif [[ -L /etc/systemd/system/$2 ]]; then echo masked
        else echo loaded; fi ;;
      *--property=FragmentPath*)
        if [[ -L /etc/systemd/system/$2 ]]; then echo "/etc/systemd/system/$2"
        elif [[ ! -e "${SYNC_TEST_EVENTS}.not-found" ]]; then echo "/run/systemd/generator/$2"; fi ;;
    esac
    ;;
esac
`,
	"chown": `printf 'chown %s\n' "$*" >>"${SYNC_TEST_EVENTS:?}"
`,
	"mv": `if [[ -f "$SYNC_TEST_EVENTS.fail-before" && "${!#}" == "$(<"$SYNC_TEST_EVENTS.fail-before")" ]]; then exit 92; fi
exec /usr/bin/mv "$@"
`,
	"podman": `[[ "$1 $2 $#" == "image exists 3" ]] || exit 64
! grep -qxF -- "$3" "$SYNC_TEST_EVENTS.image-missing" 2>/dev/null
`,
	"python3": `if [[ "$*" == '-I /opt/iris-stack/valkey-ha/stage.py selected hololive' ]]; then
  [[ ! -e "$SYNC_TEST_EVENTS.stage-denied" ]]
  exit $?
fi
exec /usr/bin/python3 "$@"
`,
}

const fakeOwnershipVerifier = `#!/usr/bin/env bash
printf 'verify %s\n' "$#" >>"${SYNC_TEST_EVENTS:?}"
printf 'verify-path %s\n' "$@" >>"${SYNC_TEST_EVENTS:?}"
if [[ -e "${SYNC_TEST_EVENTS}.ownership-denied" ]]; then
  denied="$(cat "${SYNC_TEST_EVENTS}.ownership-denied")"
  for path in "$@"; do [[ "$path" != "$denied" ]] || exit 1; done
fi
`

const syncDriver = `set -euo pipefail
host="$1"; repo="$2"; stubs="$3"; bootstrap="$4"
shift 4
mount --bind "${host}/etc-containers-systemd" /etc/containers/systemd
mount --bind "${host}/usr-local-libexec" /usr/local/libexec
mount --bind "${host}/etc-stack-secrets" /etc/stack-secrets
mount --bind "${host}/etc-systemd-system" /etc/systemd/system
mount --bind "${host}/etc-nftables.d" /etc/nftables.d
mount --bind "${host}/run" /run
mount --bind "${host}/run-lock" /run/lock
if [[ " $* " != *" --source-only "* ]]; then
  mount -t tmpfs -o mode=0755 tmpfs /opt
  mkdir -p /opt/iris-stack/valkey-ha
  : >/opt/iris-stack/valkey-ha/stage.py
fi
if [[ " $* " == *" --valkey-only "* ]]; then
  mkdir -p /opt/hololive-bot/compose
  mount --bind "$host/opt" /opt/hololive-bot/compose
  for directory in /usr/lib/systemd/system /usr/local/lib/systemd/system /usr/share/containers/systemd; do
    [[ ! -d $directory ]] || mount -t tmpfs -o mode=0755 tmpfs "$directory"
  done
fi
if [[ " $* " == *" --source-only "* ]]; then
  jail="$host/jail"
  mkdir -p "$jail"
  mount -t tmpfs -o mode=0755 tmpfs "$jail"
  mkdir -p "$jail"/{usr,dev,etc,proc,run,sys,tmp,var,opt,repo,stubs}
  for directory in dev etc proc run sys; do mount --rbind "/$directory" "$jail/$directory"; done
  for directory in bin sbin lib lib64 share libexec local/libexec; do
    mkdir -p "$jail/usr/$directory"
    mount --rbind "/usr/$directory" "$jail/usr/$directory"
  done
  chmod 0755 "$jail/usr" "$jail/usr/local"
  mount --bind "$repo" "$jail/repo"
  mount --bind "$stubs" "$jail/stubs"
  touch "$jail/bootstrap" "$jail/events"
  mount --bind "$bootstrap" "$jail/bootstrap"
  mount --bind "$host/events" "$jail/events"
  if [[ -f "$host/canary" ]]; then touch "$jail/canary"; mount --bind "$host/canary" "$jail/canary"; fi
  if [[ -e "$host/events.fail-before" ]]; then cp "$host/events.fail-before" "$jail/events.fail-before"; fi
  for directory in bin sbin lib lib64; do
    [[ -e "$jail/$directory" ]] || ln -s "usr/$directory" "$jail/$directory"
  done
  mount --bind "$host/opt" "$jail/opt"
  exec chroot "$jail" env PATH=/stubs:/usr/bin:/usr/sbin HOME=/tmp TMPDIR=/tmp SYNC_TEST_EVENTS=/events REPO_ROOT=/repo OPT_ROOT=/opt bash ./bootstrap "${@: -2}"
fi
exec "$@"
`

var repoPaths = []string{
	"scripts/deploy/lib/compose-env.sh", "scripts/deploy/quadlet-prestart.sh", "scripts/deploy/sync-opt-current.sh",
	"scripts/deploy/quadlet-redeploy-service.sh", "scripts/deploy/quadlet-rollback-service.sh",
	"scripts/deploy/lib/writer-drain-guard.py", "scripts/deploy/lib/worker-profile-pair.py", "scripts/deploy/lib/systemd-stop-guard.py",
	"scripts/deploy/lib/postgres-capacity.sh", "scripts/ci/postgres-capacity-policy.tsv",
	"scripts/deploy/lib/admin-bind.sh", "scripts/deploy/lib/public-bind-mounts.sh",
	"scripts/deploy/lib/postgres-endpoint.sh", "scripts/deploy/lib/source-revision.sh",
	"scripts/systemd/admin-dashboard-ingress-firewall.service", "scripts/systemd/admin-dashboard-ingress.nft",
	"verification/tla/AlarmDispatchClaim/COVERAGE.md", "verification/tla/CollectionJobLease/COVERAGE.md",
	"hololive/hololive-api/scripts/init-db", "hololive/hololive-api/cmd/db-migrate/queries/bootstrap_writer_admission.sql",
	"deploy", "scripts/deploy/lib/collector-slots.sh", "scripts/deploy/lib/collector-slots.tsv",
}

type syncEnv struct {
	tmp, fakebin string
}

func newSyncEnv(t *testing.T) syncEnv {
	t.Helper()

	if info, err := os.Stat(quadletGenerator); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("quadlet generator missing: %s", quadletGenerator)
	}

	if err := exec.CommandContext(t.Context(), "podman", "unshare", "unshare", "--mount", "true").Run(); err != nil {
		t.Fatalf("rootless Podman user+mount namespace unavailable: %v", err)
	}

	e := syncEnv{tmp: t.TempDir()}

	e.fakebin = filepath.Join(e.tmp, "fakebin")

	for name, body := range syncFakes {
		harness.Executable(t, filepath.Join(e.fakebin, name), body)
	}

	return e
}

func run(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...) // #nosec G204 -- fixture가 고정된 도구만 실행한다.

	cmd.Dir = dir
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}

	return strings.TrimSpace(string(out))
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	run(t, "", "cp", "-a", src, dst)
}

func commit(t *testing.T, repo, message string) {
	t.Helper()
	run(t, repo, "git", "add", "-A")
	run(t, repo, "git", "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath=/dev/null",
		"-c", "commit.gpgsign=false", "commit", "-qm", message)
}

func head(t *testing.T, repo string) string {
	t.Helper()

	return run(t, repo, "git", "rev-parse", "HEAD")
}

func shortHead(t *testing.T, repo string) string {
	t.Helper()

	return run(t, repo, "git", "rev-parse", "--short", "HEAD")
}

func (e syncEnv) repo(t *testing.T, name string) string {
	t.Helper()

	repo := filepath.Join(e.tmp, name)

	for _, rel := range repoPaths {
		dst := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { // #nosec G301 -- 배포 트리 권한을 재현한다.
			t.Fatal(err)
		}

		run(t, "", "cp", "-R", harness.Path(t, rel), dst)
	}

	appendFile(t, filepath.Join(repo, "scripts/deploy/sync-opt-current.sh"), "\n")
	writeExec(t, filepath.Join(repo, "scripts/deploy/verify-exec-tree-ownership.sh"), fakeOwnershipVerifier)
	run(t, repo, "git", "init", "-q")
	commit(t, repo, "fixture")

	return repo
}

func (e syncEnv) clone(t *testing.T, src, name string) string {
	t.Helper()

	dst := filepath.Join(e.tmp, name)
	copyTree(t, src, dst)

	return dst
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { // #nosec G306 -- 실행 파일 권한을 재현한다.
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) // #nosec G304 -- fixture 경로만 연다.
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func put(t *testing.T, path, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- 호스트 디렉터리 권한을 재현한다.
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil { // #nosec G306 -- 설치 파일 권한을 재현한다.
		t.Fatal(err)
	}
}

func overwrite(t *testing.T, src, dst string) {
	t.Helper()

	data, err := os.ReadFile(src) // #nosec G304 -- fixture 경로만 읽는다.
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, 0) // #nosec G304 -- fixture 경로만 연다.
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()

	return bytes.Equal([]byte(harness.Read(t, a)), []byte(harness.Read(t, b)))
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()

	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil { // #nosec G301 -- 호스트 디렉터리 권한을 재현한다.
			t.Fatal(err)
		}
	}
}

func removeAll(t *testing.T, path string) {
	t.Helper()

	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
}

type syncHost string

func (h syncHost) at(rel string) string {
	return filepath.Join(string(h), filepath.FromSlash(rel))
}

func (h syncHost) installed(name string) string {
	return h.at("etc-containers-systemd/hololive-bot/" + name)
}

func (h syncHost) compose() string {
	return h.at("etc-stack-secrets/hololive-bot/compose.env")
}

func (e syncEnv) host(t *testing.T, name, xSpaces string) syncHost {
	t.Helper()

	h := syncHost(filepath.Join(e.tmp, name))
	mkdirs(t, h.at("etc-containers-systemd"), h.at("usr-local-libexec"), h.at("etc-stack-secrets/hololive-bot"),
		h.at("etc-systemd-system"), h.at("etc-nftables.d"), h.at("run-lock"), h.at("run/hololive-bot"), h.at("run/lock"))

	if err := os.Mkdir(h.at("run/mount"), 0o700); err != nil {
		t.Fatal(err)
	}

	put(t, h.at("run/hololive-bot/marker"), "accepted runtime state\n")
	put(t, h.installed("stale.container"), "[Container]\nContainerName=stale\nImage=localhost/stale:old\n")
	put(t, h.installed("hololive-api.container.d/50-worker-profile.conf"), "[Container]\nLabel=io.hololive.test.worker-profile=1\n")
	put(t, h.installed("hololive-alarm-worker.container.d/40-x-spaces.conf"), "# old\n[Container]\nEnvironment=X_SPACES_OLD=1\n")
	put(t, h.at("opt/current/marker"), "old current\n")
	put(t, h.compose(), "VALKEY_BIND_IP=100.100.1.8\nHOLOLIVE_X_SPACES_ENABLED="+xSpaces+"\n")

	return h
}

func (h syncHost) setCompose(t *testing.T, key, value string) {
	t.Helper()

	var lines []string

	for line := range strings.Lines(harness.Read(t, h.compose())) {
		if !strings.HasPrefix(line, key+"=") {
			lines = append(lines, line)
		}
	}

	if value != "-" {
		lines = append(lines, key+"="+value+"\n")
	}

	overwriteText(t, h.compose(), strings.Join(lines, ""))
}

func overwriteText(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0) // #nosec G304 -- fixture 경로만 연다.
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func (e syncEnv) sync(t *testing.T, h syncHost, repo string, args ...string) harness.Result {
	t.Helper()

	return e.syncOpt(t, h, repo, "", args...)
}

func (e syncEnv) syncOpt(t *testing.T, h syncHost, repo, optRoot string, args ...string) harness.Result {
	t.Helper()

	opt := h.at("opt")

	switch {
	case optRoot != "":
		opt = optRoot
	case len(args) > 0 && args[0] == "--source-only":
		opt = "/opt"
	case len(args) > 0 && args[0] == "--valkey-only":
		opt = "/opt/hololive-bot/compose"
	}

	script := harness.Path(t, "scripts/deploy/sync-opt-current.sh")
	driverArgs := append([]string{
		string(h), repo, e.fakebin, script, "env", "PATH=" + e.fakebin + ":" + os.Getenv("PATH"), "SYNC_TEST_EVENTS=" + h.at("events"),
		"REPO_ROOT=" + repo, "OPT_ROOT=" + opt, "bash", script,
	}, args...)

	return harness.Bash(t, harness.Cmd{
		Script: `: >"$1/events"
podman unshare unshare --mount bash -c "$SYNC_DRIVER" _ "$@"`,
		Args: driverArgs,
		Env:  []string{"SYNC_DRIVER=" + syncDriver},
	})
}

func (e syncEnv) syncs(t *testing.T, h syncHost, repo, label string, args ...string) {
	t.Helper()
	harness.Succeeds(t, label, e.sync(t, h, repo, args...))
}

func (h syncHost) events(t *testing.T) string {
	t.Helper()

	return harness.Read(t, h.at("events"))
}

func (h syncHost) hasEvent(t *testing.T, line string) bool {
	t.Helper()

	return slices.Contains(strings.Split(h.events(t), "\n"), line)
}

func (h syncHost) snapshot(t *testing.T, args ...string) string {
	t.Helper()

	opt := "opt/current"

	if len(args) > 0 && (args[0] == "--application-only" || args[0] == "--source-only") {
		opt = "opt"
	}

	r := harness.Bash(t, harness.Cmd{
		Script: `cd "$1" && find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run "$2" \
  -printf '%p %m %U:%G %i\n' -type f -printf '%p %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort`,
		Args: []string{string(h), opt},
	})
	harness.Succeeds(t, "snapshot", r)

	return r.Stdout
}

const unitsDoNotGenerate = "staged Quadlet units do not generate; nothing was installed"

func (e syncEnv) rejects(t *testing.T, h syncHost, repo, label, expected string, args ...string) {
	t.Helper()
	e.rejectsOpt(t, h, repo, "", label, expected, args...)
}

func (e syncEnv) rejectsOpt(t *testing.T, h syncHost, repo, optRoot, label, expected string, args ...string) {
	t.Helper()

	before := h.snapshot(t, args...)
	r := e.syncOpt(t, h, repo, optRoot, args...)
	harness.Fails(t, label, r)

	if after := h.snapshot(t, args...); after != before {
		t.Fatalf("%s: host state changed after a rejected dry-run", label)
	}

	if strings.Contains(h.events(t), "daemon-reload") {
		t.Fatalf("%s: daemon-reload ran after a rejected dry-run", label)
	}

	if !strings.Contains(r.Stdout+r.Stderr, expected) {
		t.Fatalf("%s: rejection did not come from the expected check %q:\n%s", label, expected, r)
	}

	for _, pattern := range []string{".staging-current.*", ".quadlet-check.*"} {
		if left := glob(t, h.at("opt/"+pattern)); len(left) > 0 {
			t.Fatalf("%s: temporary staging directories were left behind: %v", label, left)
		}
	}
}

func backups(t *testing.T, h syncHost, prefix string) []string {
	t.Helper()

	return glob(t, h.at("opt/current.bak-"+prefix+"*"))
}

func glob(t *testing.T, pattern string) []string {
	t.Helper()

	found, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}

	return found
}

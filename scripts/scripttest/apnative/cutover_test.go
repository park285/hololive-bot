package apnative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const cutoverCandidateEnv = "POSTGRES_HOST=100.100.1.8\nPOSTGRES_PORT=5433\n"

const cutoverRestartAction = `native_cutover_run restart sudo -n systemctl restart "$unit"; touch "$CASE/activated"`

type cutoverEnv struct {
	helper, endpointLibs, functions, logCheck, gate, originDispatch, slotPreamble string
}

func sourceBetween(t *testing.T, source, start, end string) string {
	t.Helper()

	i := strings.Index(source, start)
	if i < 0 {
		t.Fatalf("extraction anchor %q missing", start)
	}

	j := strings.Index(source[i:], end)
	if j < 0 {
		t.Fatalf("extraction anchor %q missing", end)
	}

	return source[i : i+j]
}

func newCutoverEnv(t *testing.T) cutoverEnv {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble a`})
	harness.Succeeds(t, "collector slot preamble", r)

	preamble := r.Stdout
	helper := filepath.Join(t.TempDir(), "slot-cutover.sh")
	harness.Write(t, helper, preamble+`. "`+harness.Path(t, "scripts/deploy/lib/ap-host-native-cutover.sh")+"\"\n")

	libs := preamble

	for _, name := range []string{"compose-env.sh", "postgres-endpoint.sh", "ap-host-native-rollback-check.sh"} {
		libs += `. "` + harness.Path(t, "scripts/deploy/lib/"+name) + "\"\n"
	}

	source := harness.Read(t, harness.Path(t, "scripts/deploy/lib/ap-host-native-remote-apply.sh"))
	gateStart := "native_pool_check_pending=true\n"

	return cutoverEnv{
		helper:         helper,
		endpointLibs:   libs,
		functions:      sourceBetween(t, source, "native_same_window_origin()", "\narm_native_cutover_restore\n"),
		logCheck:       sourceBetween(t, source, "native_post_cutover_log_check()", "\nnative_post_cutover_log_check\n"),
		gate:           sourceBetween(t, source, gateStart, "\nsudo -n systemd-analyze verify \"$unit_file\"\n"),
		originDispatch: sourceBetween(t, source, "native_same_window_baseline()", "\nnormalize_runtime_payload_permissions()"),
		slotPreamble:   preamble,
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func readInt(t *testing.T, path string) int {
	t.Helper()

	n, err := strconv.Atoi(strings.TrimSpace(harness.Read(t, path)))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	return n
}

func resolvesTo(t *testing.T, link, want string) bool {
	t.Helper()

	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		return false
	}

	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("resolve %s: %v", want, err)
	}

	return got == wantResolved
}

type fixtureProc struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

func (p *fixtureProc) awaitFile(t *testing.T, path string) {
	t.Helper()

	p.await(t, path, func() bool { return exists(path) })
}

func (p *fixtureProc) awaitWorker(t *testing.T, path string) int {
	t.Helper()

	n := 0

	p.await(t, path, func() bool {
		if !exists(path) {
			return false
		}

		parsed, err := strconv.Atoi(strings.TrimSpace(harness.Read(t, path)))

		n = parsed

		return err == nil
	})

	return n
}

func (p *fixtureProc) await(t *testing.T, path string, ready func() bool) {
	t.Helper()

	changes := watchDir(t, filepath.Dir(path))
	deadline := time.After(5 * time.Second)

	for !ready() {
		select {
		case <-changes:
		case <-p.done:
			if ready() {
				return
			}

			t.Fatalf("fixture exited before %s: %v: %s", filepath.Base(path), p.err, harness.Read(t, filepath.Join(filepath.Dir(path), "err")))
		case <-deadline:
			t.Fatalf("fixture did not reach %s", filepath.Base(path))
		}
	}
}

func watchDir(t *testing.T, dir string) <-chan struct{} {
	t.Helper()

	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatalf("inotify: %v", err)
	}

	watch := os.NewFile(uintptr(fd), "inotify")
	if _, err := syscall.InotifyAddWatch(fd, dir, syscall.IN_CREATE|syscall.IN_MOVED_TO|syscall.IN_CLOSE_WRITE|syscall.IN_MODIFY); err != nil {
		t.Fatalf("watch %s: %v", dir, err)
	}

	changes := make(chan struct{}, 1)
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		buf := make([]byte, 4096)

		for {
			if _, err := watch.Read(buf); err != nil {
				return
			}

			select {
			case changes <- struct{}{}:
			default:
			}
		}
	}()

	t.Cleanup(func() {
		if err := watch.Close(); err != nil {
			t.Errorf("close inotify: %v", err)
		}

		<-stopped
	})

	return changes
}

func (p *fixtureProc) status(t *testing.T) int {
	t.Helper()

	select {
	case <-p.done:
	case <-time.After(12 * time.Second):
		t.Fatal("fixture did not finish within 12s")
	}

	if p.err == nil {
		return 0
	}

	exitErr, ok := errors.AsType[*exec.ExitError](p.err)
	if !ok {
		t.Fatalf("wait fixture: %v", p.err)
	}

	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -int(ws.Signal())
	}

	return exitErr.ExitCode()
}

type signalCase struct {
	name, action, target, mode string
	signo                      syscall.Signal
}

func (e cutoverEnv) runSignalCase(t *testing.T, c signalCase) (string, int) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), c.name)
	harness.Write(t, filepath.Join(dir, "new/collector-host.env"), cutoverCandidateEnv)
	harness.Write(t, filepath.Join(dir, "old/rollback-contract/collector-host.env"), cutoverCandidateEnv)
	harness.Executable(t, filepath.Join(dir, "bin/systemctl"), strings.TrimPrefix(cutoverSystemctlFake, "#!/usr/bin/env bash\n"))

	script := filepath.Join(dir, "run.sh")
	harness.Write(t, script, cutoverRunPrefix+`. "`+e.helper+"\"\n"+e.endpointLibs+cutoverRunVars+e.functions+cutoverRunStubs+c.action+"\n")

	mode := c.mode
	if mode == "" {
		mode = normalMode
	}

	phase := ""

	if c.target != "" {
		phase = "restart"

		if strings.HasPrefix(c.target, "restore") {
			phase = "restore"
		}
	}

	out, err := os.Create(filepath.Join(dir, "out")) // #nosec G304 -- 테스트가 만든 fixture 경로다.
	if err != nil {
		t.Fatalf("create out: %v", err)
	}
	defer out.Close()

	errFile, err := os.Create(filepath.Join(dir, "err")) // #nosec G304 -- 테스트가 만든 fixture 경로다.
	if err != nil {
		t.Fatalf("create err: %v", err)
	}
	defer errFile.Close()

	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), "bash", script) // #nosec G204 -- 테스트가 만든 fixture 스크립트만 실행한다.

	cmd.Env = append(os.Environ(), "CASE="+dir, "MODE="+mode, "SIGNAL_PHASE="+phase,
		"PATH="+filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdout = out
	cmd.Stderr = errFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start fixture: %v", err)
	}

	p := &fixtureProc{cmd: cmd, done: make(chan struct{})}

	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	t.Cleanup(func() { killGroup(t, cmd.Process.Pid) })

	if c.target != "" {
		e.deliver(t, p, dir, c)
	}

	return dir, p.status(t)
}

const (
	normalMode    = "normal"
	childTarget   = "child"
	failingAction = "false"
)

func signal(t *testing.T, pid int, signo syscall.Signal) {
	t.Helper()

	if err := syscall.Kill(pid, signo); err != nil {
		t.Fatalf("signal %d: %v", pid, err)
	}
}

func killGroup(t *testing.T, pgid int) {
	t.Helper()

	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		t.Errorf("kill group %d: %v", pgid, err)
	}
}

func (cutoverEnv) deliver(t *testing.T, p *fixtureProc, dir string, c signalCase) {
	t.Helper()

	marker := "entered"

	if strings.HasPrefix(c.target, "restore") {
		marker = "restoring"
	}

	p.awaitFile(t, filepath.Join(dir, marker))

	workerFile := filepath.Join(dir, "recovery/worker-pid")

	switch {
	case strings.HasSuffix(c.target, "group"):
		worker := p.awaitWorker(t, workerFile)

		if exists(filepath.Join(dir, "command.pid")) {
			if pgid, err := syscall.Getpgid(readInt(t, filepath.Join(dir, "command.pid"))); err == nil && pgid != worker {
				t.Fatalf("restart command runs in group %d, want worker group %d", pgid, worker)
			}
		}

		signal(t, -worker, c.signo)
		signal(t, p.cmd.Process.Pid, c.signo)
	case c.target == "worker":
		signal(t, p.awaitWorker(t, workerFile), c.signo)
	case c.target == childTarget:
		signal(t, readInt(t, filepath.Join(dir, "command.pid")), c.signo)
	default:
		signal(t, p.cmd.Process.Pid, c.signo)
	}

	harness.Write(t, filepath.Join(dir, "signal-delivered"), "")
}

func (e cutoverEnv) requireStillBlocked(t *testing.T, dir string) {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: `sudo() { shift; "$@"; }; . "$1"; native_recovery_dir="$2"; native_recovery_require_clear`, Args: []string{e.helper, filepath.Join(dir, "recovery")}})
	harness.Fails(t, "a retained recovery guard must block another apply", r)
}

func enableNowCount(t *testing.T, dir string) int {
	t.Helper()

	return strings.Count(harness.Read(t, filepath.Join(dir, "calls")), "enable --now ")
}

func TestOwnerSignalsRestoreTheRecordedRuntimeOnce(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		signo  syscall.Signal
		status int
	}{{syscall.SIGHUP, 129}, {syscall.SIGINT, 130}, {syscall.SIGTERM, 143}} {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			t.Parallel()

			dir, status := e.runSignalCase(t, signalCase{name: "owner", action: cutoverRestartAction, target: "owner", signo: c.signo})
			if status != c.status || !exists(filepath.Join(dir, "completed")) || !exists(filepath.Join(dir, "restored")) {
				t.Fatalf("status=%d, want %d with a completed restart and a restore: %s", status, c.status, harness.Read(t, filepath.Join(dir, "err")))
			}

			if exists(filepath.Join(dir, "activated")) || exists(filepath.Join(dir, "recovery")) || enableNowCount(t, dir) != 1 {
				t.Fatal("an owner signal must restore once, release the guard and never activate")
			}
		})
	}
}

func TestChildOrWorkerSignalsLeaveTheOutcomeUnknown(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		target string
		signo  syscall.Signal
		status int
	}{
		{childTarget, syscall.SIGTERM, 143},
		{childTarget, syscall.SIGINT, 130},
		{childTarget, syscall.SIGHUP, 129},
		{"group", syscall.SIGINT, 130},
		{"worker", syscall.SIGKILL, 137},
	} {
		t.Run(c.target+"-"+strconv.Itoa(c.status), func(t *testing.T) {
			t.Parallel()

			dir, status := e.runSignalCase(t, signalCase{name: c.target, action: cutoverRestartAction, target: c.target, signo: c.signo})
			if status != c.status {
				t.Fatalf("status=%d, want %d: %s", status, c.status, harness.Read(t, filepath.Join(dir, "err")))
			}

			if exists(filepath.Join(dir, "restored")) || exists(filepath.Join(dir, "activated")) {
				t.Fatal("an interrupted worker must neither restore nor activate")
			}

			if !strings.Contains(harness.Read(t, filepath.Join(dir, "err")), "outcome_unknown") || harness.Mode(t, filepath.Join(dir, "recovery")).Perm() != 0o700 {
				t.Fatal("an interrupted worker must keep a private recovery guard with outcome_unknown")
			}

			e.requireStillBlocked(t, dir)
		})
	}
}

func TestRestorePhaseOutcomes(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		signalCase

		status            int
		recovery, restore bool
		message           string
	}{
		{signalCase{name: "known-failure", action: `native_cutover_run failed bash -c "exit 7"`}, 7, false, true, ""},
		{signalCase{name: "restore-owner", action: failingAction, target: "restore-owner", signo: syscall.SIGTERM}, 143, false, true, ""},
		{signalCase{name: "restore-failure-signal", action: failingAction, target: "restore-owner", signo: syscall.SIGTERM, mode: "restore_failure"}, 143, true, false, "could not be restored"},
		{signalCase{name: "restore-prelaunch-signal", action: failingAction, mode: "restore_prelaunch"}, 143, true, false, "could not be restored"},
		{signalCase{name: "restore-release-failure", action: failingAction, target: "restore-owner", signo: syscall.SIGTERM, mode: "release_failure"}, 143, true, true, "could not be released"},
		{signalCase{name: "restore-group", action: failingAction, target: "restore-group", signo: syscall.SIGTERM}, 143, true, false, "outcome_unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			dir, status := e.runSignalCase(t, c.signalCase)
			stderr := harness.Read(t, filepath.Join(dir, "err"))

			if status != c.status || exists(filepath.Join(dir, "recovery")) != c.recovery || exists(filepath.Join(dir, "restored")) != c.restore {
				t.Fatalf("status=%d recovery=%t restored=%t, want %d %t %t: %s", status, exists(filepath.Join(dir, "recovery")), exists(filepath.Join(dir, "restored")), c.status, c.recovery, c.restore, stderr)
			}

			if !strings.Contains(stderr, c.message) {
				t.Fatalf("stderr lacks %q: %s", c.message, stderr)
			}

			if c.target != "" && enableNowCount(t, dir) != 1 {
				t.Fatal("restoration must start exactly once")
			}
		})
	}
}

func TestGuardWriteOrReadFailureLeavesTheOutcomeUnknown(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for mode, want := range map[string]int{"write_failure": 9, "read_failure": 8} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			dir, status := e.runSignalCase(t, signalCase{name: mode, action: cutoverRestartAction, mode: mode})
			if status != want || !exists(filepath.Join(dir, "recovery")) || exists(filepath.Join(dir, "restored")) || exists(filepath.Join(dir, "activated")) {
				t.Fatalf("status=%d, want %d with the guard retained and nothing restored", status, want)
			}

			if !strings.Contains(harness.Read(t, filepath.Join(dir, "err")), "outcome_unknown") {
				t.Fatal("a guard I/O failure must report outcome_unknown")
			}

			if terminal := filepath.Join(dir, "recovery/terminal"); exists(terminal) && harness.Mode(t, terminal).Perm() != 0o600 {
				t.Fatal("the terminal receipt must stay private")
			}
		})
	}
}

func TestStalledOwnerKeepsTheGuard(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	dir, status := e.runSignalCase(t, signalCase{name: "stalled-owner", action: cutoverRestartAction, target: "owner", signo: syscall.SIGTERM, mode: "stalled"})
	t.Cleanup(func() { killGroup(t, readInt(t, filepath.Join(dir, "recovery/worker-pid"))) })

	if status != 143 || !exists(filepath.Join(dir, "recovery")) || exists(filepath.Join(dir, "restored")) || exists(filepath.Join(dir, "activated")) {
		t.Fatalf("status=%d: a stalled worker must keep the guard without restore or activation", status)
	}
}

func TestFailedDatabaseProofNeverRestartsAnything(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	dir, status := e.runSignalCase(t, signalCase{name: "failed-database-proof", action: cutoverFailedDatabaseProof + e.gate})

	if status != 7 || !exists(filepath.Join(dir, "recovery")) {
		t.Fatalf("status=%d: a failed endpoint proof must keep the guard", status)
	}

	calls := strings.Split(strings.TrimSpace(harness.Read(t, filepath.Join(dir, "calls"))), "\n")
	probes := 0

	for _, line := range calls {
		if line == "probe" {
			probes++
		}

		for _, prefix := range []string{"enable ", "restart ", "start ", "po_restore_previous"} {
			if strings.HasPrefix(line, prefix) {
				t.Fatalf("a failed endpoint proof must not change units: %q", line)
			}
		}
	}

	if probes != 1 {
		t.Fatalf("endpoint probe ran %d times, want 1", probes)
	}
}

func TestRecoveryGuardClaimAndRelease(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, occupied := range []bool{false, true} {
		dir := t.TempDir()

		if occupied {
			harness.Write(t, filepath.Join(dir, "recovery/.keep"), "")
		}

		r := harness.Bash(t, harness.Cmd{Script: `sudo() { shift; "$@"; }; . "$1"; native_recovery_dir="$2/recovery"; native_recovery_require_clear && touch "$2/changed"`, Args: []string{e.helper, dir}})
		if (r.ExitCode != 0) != occupied || exists(filepath.Join(dir, "changed")) == occupied {
			t.Fatalf("occupied=%t: %s", occupied, r)
		}
	}

	for kind, mutate := range map[string]string{
		"foreign":    `printf other > "$native_recovery_dir/claim"`,
		"unknown":    `touch "$native_recovery_dir/outcome"`,
		"unexpected": `touch "$native_recovery_dir/foreign"`,
		childTarget:  ":",
	} {
		action := "native_cutover_release_guard"

		if kind == childTarget {
			action = "(native_cutover_release_guard)"
		}

		guard := filepath.Join(t.TempDir(), "recovery")
		r := harness.Bash(t, harness.Cmd{Script: `sudo() { shift; "$@"; }; . "$1"; native_recovery_dir="$2"; native_cutover_claim first; ` + mutate + "; " + action, Args: []string{e.helper, guard}})

		if r.ExitCode == 0 || !exists(filepath.Join(guard, "claim")) || strings.TrimSpace(harness.Read(t, filepath.Join(guard, "release"))) != "first" {
			t.Fatalf("%s: a guard that is not exclusively ours must survive release: %s", kind, r)
		}
	}
}

func TestExistingHostCheckRejectsDrift(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	dir := t.TempDir()
	harness.Write(t, filepath.Join(dir, "verify.py"), cutoverHostVerifyPy)

	files := map[string]string{
		"hololive-bot":                     "native_logrotate_body",
		"99-hololive-quic-udp-buffer.conf": "native_udp_body 7500000",
		"99-hololive-swap.conf":            "native_swap_body",
	}
	for name, body := range files {
		r := harness.Bash(t, harness.Cmd{Script: `. "$1"; ` + body, Args: []string{e.helper}})
		harness.Succeeds(t, body, r)
		harness.Write(t, filepath.Join(dir, name), r.Stdout)
	}

	check := func(fault string) harness.Result {
		return harness.Bash(t, harness.Cmd{Script: cutoverHostCheck, Args: []string{e.helper, dir}, Env: []string{"FAULT=" + fault}})
	}

	harness.Succeeds(t, "baseline host", check(normalMode))

	for name := range files {
		path := filepath.Join(dir, name)
		saved := harness.Read(t, path)
		harness.Write(t, path, saved+"\n")
		harness.Fails(t, name+" drift", check(normalMode))

		if harness.Read(t, path) != saved+"\n" {
			t.Fatalf("the host check rewrote %s", name)
		}

		harness.Write(t, path, saved)
	}

	for _, fault := range []string{"runtime", "inactive", "signature", "directory", "swap-mode", "swap-size", "fstab-link", "fstab-duplicate", "fstab-options", "unit-owner", "user"} {
		harness.Fails(t, fault, check(fault))
	}

	if exists(filepath.Join(dir, "unexpected-command")) {
		t.Fatal("the host check ran an unexpected privileged command")
	}
}

func ctimeNs(t *testing.T, path string) int64 {
	t.Helper()

	var st syscall.Stat_t

	if err := syscall.Stat(path, &st); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return st.Ctim.Nano()
}

func statIdentity(t *testing.T, path string) [4]int64 {
	t.Helper()

	var st syscall.Stat_t

	if err := syscall.Stat(path, &st); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	return [4]int64{int64(st.Mode), int64(st.Uid), int64(st.Gid), st.Mtim.Nano()}
}

type releasePinFixture struct {
	helper, field, name, key, live, old, next string
}

func (f releasePinFixture) pin(t *testing.T, action, expected string) int {
	t.Helper()

	sum := sha256.Sum256([]byte(expected))

	return harness.Bash(t, harness.Cmd{
		Script: `. "$1"; shift; sudo() { shift; "$@"; }; native_release_pin "$@"`,
		Args:   []string{f.helper, action, f.field, f.live, f.old, f.next, hex.EncodeToString(sum[:])},
	}).ExitCode
}

func newReleasePinFixture(t *testing.T, helper, field, name, key, tail string) (f releasePinFixture, prior, candidate string) {
	t.Helper()

	dir := t.TempDir()

	f = releasePinFixture{
		helper: helper, field: field, name: name, key: key,
		live: filepath.Join(dir, name), old: filepath.Join(dir, "old"), next: filepath.Join(dir, "new-longer-id"),
	}
	prior = "BEFORE=yes\n" + key + f.old + tail + "\nAFTER=no"
	candidate = strings.ReplaceAll(prior, f.old, f.next)
	harness.Write(t, filepath.Join(f.old, name), prior)
	harness.Write(t, filepath.Join(f.next, name), candidate)
	harness.Write(t, f.live, prior)

	mode := os.FileMode(0o644)

	if field == "profile" {
		mode = 0o640
	}

	if err := os.Chmod(f.live, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := syscall.Setxattr(f.live, "user.native-fixture", []byte("kept"), 0); err != nil {
		t.Fatalf("setxattr: %v", err)
	}

	return f, prior, candidate
}

func (f releasePinFixture) requireRejectsUnpinned(t *testing.T, prior, candidate string) {
	t.Helper()

	before := ctimeNs(t, f.live)
	if f.pin(t, "check", candidate) != 0 || ctimeNs(t, f.live) != before {
		t.Fatal("check must accept the pinned candidate without touching the live file")
	}

	for _, bad := range []string{candidate + "\n", candidate + "\n" + f.key + "other", strings.Replace(candidate, "BEFORE=yes", "BEFORE=no", 1)} {
		harness.Write(t, filepath.Join(f.next, f.name), bad)

		if f.pin(t, "apply", bad) == 0 || f.pin(t, "apply", candidate) == 0 || ctimeNs(t, f.live) != before || harness.Read(t, f.live) != prior {
			t.Fatalf("an unpinned candidate was applied: %q", bad)
		}
	}

	harness.Write(t, filepath.Join(f.next, f.name), candidate)
	harness.Write(t, f.live, prior+"\n")

	if f.pin(t, "check", candidate) == 0 || f.pin(t, "apply", candidate) == 0 || f.pin(t, "restore", candidate) == 0 {
		t.Fatal("a drifted live file must refuse every pin action")
	}

	harness.Write(t, f.live, prior)
}

func (f releasePinFixture) requireInPlaceRewrite(t *testing.T, action, candidate, want string, identity [4]int64) {
	t.Helper()

	if f.pin(t, action, candidate) != 0 || harness.Read(t, f.live) != want {
		t.Fatalf("%s did not produce the pinned contents", action)
	}

	if statIdentity(t, f.live) != identity {
		t.Fatalf("%s changed mode, owner or mtime", action)
	}

	buf := make([]byte, 16)

	n, err := syscall.Getxattr(f.live, "user.native-fixture", buf)
	if err != nil || string(buf[:n]) != "kept" {
		t.Fatalf("%s dropped extended attributes", action)
	}
}

func TestReleasePinRewritesOnlyTheReleasePath(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct{ field, name, key, tail string }{
		{"profile", "collector-host.env", "STACK_WORKER_PROFILE_FILE=", "/worker-profile.json"},
		{"issuer", "issuer.service", "RootDirectory=", "/po-sandbox/rootfs"},
	} {
		t.Run(c.field, func(t *testing.T) {
			t.Parallel()

			f, prior, candidate := newReleasePinFixture(t, e.helper, c.field, c.name, c.key, c.tail)
			f.requireRejectsUnpinned(t, prior, candidate)

			identity := statIdentity(t, f.live)
			f.requireInPlaceRewrite(t, "apply", candidate, candidate, identity)
			f.requireInPlaceRewrite(t, "restore", candidate, prior, identity)

			unchanged := ctimeNs(t, f.live)
			if f.pin(t, "restore", candidate) != 0 || ctimeNs(t, f.live) != unchanged {
				t.Fatal("restoring an already restored file must be a no-op")
			}
		})
	}
}

func TestPostCutoverLogCheckTreatsGrepErrorsAsFailures(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	prefix := e.logCheck + "\nunit=test.service change_started_at=2026-10-03T00:00:00Z\njournalctl() { printf 'ordinary startup\\n'; }\n"

	for code := range 3 {
		r := harness.Bash(t, harness.Cmd{Script: prefix + `grep() { return "$LOG_STATUS"; }` + "\nnative_post_cutover_log_check\n", Env: []string{"LOG_STATUS=" + strconv.Itoa(code)}})
		if (r.ExitCode == 0) != (code == 1) {
			t.Fatalf("grep status %d: %s", code, r)
		}
	}

	r := harness.Bash(t, harness.Cmd{Script: prefix + "grep_count=0\ngrep() { grep_count=$((grep_count + 1)); if (( grep_count == 1 )); then return 1; else return 2; fi; }\nnative_post_cutover_log_check\n"})
	if r.ExitCode != 2 {
		t.Fatalf("a failing second grep must surface its status: %s", r)
	}
}

func TestIssuerRestoreStopsAtAnInterruptedDisable(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	dir := t.TempDir()
	r := harness.Bash(t, harness.Cmd{Script: e.slotPreamble + `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/ap-host-native-po.sh"
sudo() { if [[ "$2" == systemctl ]]; then return 143; fi; touch "$1/restore-past-signal"; }
po_restore_previous ""
`, Args: []string{dir}})

	if r.ExitCode != 143 || exists(filepath.Join(dir, "restore-past-signal")) {
		t.Fatalf("an interrupted disable must stop restoration: %s", r)
	}
}

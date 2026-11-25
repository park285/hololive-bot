package collectorpause

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type pauseFixture struct {
	root, state, bin string
}

const fakeSystemctl = `#!/usr/bin/env bash
state="$PAUSE_STATE"
printf '%s\n' "$*" >>"$state/calls"
read_or() { if [[ -e "$state/$1" ]]; then cat "$state/$1"; else printf '%s\n' "$2"; fi; }
case "$1" in
  show)
    active="$(read_or active active)"
    case "$3" in
      LoadState) echo loaded ;;
      NeedDaemonReload) echo no ;;
      Job) echo ;;
      ActiveState) echo "$active" ;;
      SubState) if [[ "$active" == active ]]; then echo running; else echo dead; fi ;;
      MainPID) if [[ "$active" == active ]]; then echo 4242; else echo 0; fi ;;
      Result) read_or result success ;;
      NRestarts) read_or restarts 0 ;;
      ActiveEnterTimestamp) read_or entered @1 ;;
      *) exit 1 ;;
    esac
    ;;
  stop)
    echo inactive >"$state/active"
    read_or stop-result success >"$state/result"
    ;;
  start)
    echo active >"$state/active"
    echo "@$(date -u +%s)" >"$state/entered"
    if [[ -e "$state/restarts-after-start" ]]; then echo 1 >"$state/restarts"; fi
    ;;
  *) exit 1 ;;
esac
`

const fakeSudo = `#!/usr/bin/env bash
[[ "$1" != -n ]] || shift
if [[ "$1" == test ]]; then
  [[ -e "$PAUSE_STATE/recovery" ]]
  exit
fi
exec "$@"
`

const fakeSSH = `#!/usr/bin/env bash
printf '%s\n' "$*" >>"$PAUSE_STATE/ssh"
exec bash -c "${!#}"
`

func newPauseFixture(t *testing.T) pauseFixture {
	t.Helper()

	tmp := t.TempDir()
	f := pauseFixture{root: filepath.Join(tmp, "repo"), state: filepath.Join(tmp, "state"), bin: filepath.Join(tmp, "bin")}

	for _, rel := range []string{
		"scripts/deploy/lib/ap-host.sh", "scripts/deploy/lib/collector-slots.sh",
		"scripts/deploy/lib/collector-slots.tsv", "scripts/deploy/ap-hosts/osaka.conf",
	} {
		harness.Write(t, filepath.Join(f.root, rel), harness.Read(t, harness.Path(t, rel)))
	}

	harness.Executable(t, filepath.Join(f.root, "scripts/deploy/collector-pause.sh"), harness.Read(t, harness.Path(t, "scripts/deploy/collector-pause.sh")))
	harness.Executable(t, filepath.Join(f.root, "scripts/deploy/ap-completion-check.sh"),
		"#!/usr/bin/env bash\nprintf '%s %s\\n' \"$1\" \"$CHANGE_STARTED_AT\" >\"$PAUSE_STATE/completion\"\n")
	harness.Write(t, filepath.Join(tmp, "KR.key"), "")
	harness.Executable(t, filepath.Join(f.bin, "systemctl"), fakeSystemctl)
	harness.Executable(t, filepath.Join(f.bin, "sudo"), fakeSudo)
	harness.Executable(t, filepath.Join(f.bin, "sleep"), "#!/usr/bin/env bash\nexit 0\n")
	harness.Executable(t, filepath.Join(f.bin, "ssh"), fakeSSH)

	if err := os.MkdirAll(f.state, 0o750); err != nil {
		t.Fatal(err)
	}

	return f
}

func (f pauseFixture) set(t *testing.T, name, value string) {
	t.Helper()
	harness.Write(t, filepath.Join(f.state, name), value+"\n")
}

func (f pauseFixture) run(t *testing.T, env []string, args ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `exec "$@"`,
		Args:   append([]string{filepath.Join(f.root, "scripts/deploy/collector-pause.sh")}, args...),
		Bin:    f.bin,
		Env: append([]string{
			"PAUSE_STATE=" + f.state, "APPROVE_COLLECTOR_PAUSE=true",
			"SSH_KEY=" + filepath.Join(filepath.Dir(f.root), "KR.key"),
		}, env...),
	})
}

func (f pauseFixture) calls(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(f.state, "calls"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	return string(data)
}

func (f pauseFixture) stopped(t *testing.T, unit string) bool {
	t.Helper()

	return slices.Contains(strings.Split(f.calls(t), "\n"), "stop "+unit)
}

func TestStopProvesTheCollectorStoppedCleanly(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	r := f.run(t, nil, "d", "stop")
	harness.Succeeds(t, "stop d", r)

	if !strings.Contains(r.Stdout, "[OUTCOME] stopped hololive-collector-d.service") || !f.stopped(t, "hololive-collector-d.service") {
		t.Errorf("stop did not stop the slot d collector: %s\n%s", r, f.calls(t))
	}
}

func TestStopRefusesAnInactiveCollector(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	f.set(t, "active", "inactive")
	harness.Fails(t, "stop inactive d", f.run(t, nil, "d", "stop"))

	if f.stopped(t, "hololive-collector-d.service") {
		t.Errorf("stop touched an inactive collector: %s", f.calls(t))
	}
}

func TestStopFailsWhenTheCollectorExitsUncleanly(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	f.set(t, "stop-result", "exit-code")

	r := f.run(t, nil, "d", "stop")
	harness.Fails(t, "unclean stop of d", r)

	if strings.Contains(r.Stdout, "[OUTCOME]") {
		t.Errorf("unclean stop reported an outcome: %s", r)
	}
}

func TestStartRequiresTheCollectorToStayUp(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	f.set(t, "active", "inactive")
	harness.Succeeds(t, "start d", f.run(t, nil, "d", "start"))

	g := newPauseFixture(t)
	g.set(t, "active", "inactive")
	g.set(t, "restarts-after-start", "")

	r := g.run(t, nil, "d", "start")
	harness.Fails(t, "start d with a restart", r)

	if !strings.Contains(r.Stderr, "did not stay running") {
		t.Errorf("restart after start was not reported: %s", r)
	}
}

func TestSlotARefusesNativeCutoverRecoveryState(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	f.set(t, "recovery", "")
	harness.Fails(t, "stop a under recovery", f.run(t, nil, "a", "stop"))

	if f.stopped(t, "hololive-collector@collector-a.service") {
		t.Errorf("stop ran under native cutover recovery state: %s", f.calls(t))
	}
}

func TestSlotAStartRunsTheCompletionCheckFromTheStartTime(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	f.set(t, "active", "inactive")
	harness.Succeeds(t, "start a", f.run(t, nil, "a", "start"))

	completion := strings.TrimSpace(harness.Read(t, filepath.Join(f.state, "completion")))
	if !strings.HasPrefix(completion, "osaka 20") || !strings.HasSuffix(completion, "Z") {
		t.Errorf("completion check did not receive the remote start time: %q", completion)
	}
}

func TestSlotsBAndCRequireTheirSSHTarget(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	harness.Fails(t, "stop b without target", f.run(t, nil, "b", "stop"))
	harness.Succeeds(t, "stop c with target", f.run(t, []string{"PO_C_SSH_TARGET=central"}, "c", "stop"))

	if !strings.Contains(harness.Read(t, filepath.Join(f.state, "ssh")), "central") || !f.stopped(t, "hololive-collector-c.service") {
		t.Error("slot c did not stop its collector on its SSH target")
	}
}

func TestPauseRequiresApproval(t *testing.T) {
	t.Parallel()

	f := newPauseFixture(t)
	harness.Fails(t, "stop without approval", f.run(t, []string{"APPROVE_COLLECTOR_PAUSE=false"}, "d", "stop"))

	if f.calls(t) != "" {
		t.Errorf("unapproved pause called systemctl: %s", f.calls(t))
	}
}

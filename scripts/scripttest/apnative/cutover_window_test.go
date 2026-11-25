package apnative

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const issuerSocket, issuerService, collectorService = "issuer.socket", "issuer.service", "collector.service"

const (
	ordinaryScenario        = cutoverApplyOrigin + cutoverOrdinary
	ordinaryStoppedScenario = ordinaryScenario + cutoverOrdinaryStopped
	applyStageScenario      = cutoverApplyOrigin + cutoverApplyStage
)

func (e cutoverEnv) windowCase(t *testing.T, fault, scenario string) (string, harness.Result) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "case")
	libs := ""

	for _, name := range []string{"ap-host-native-release-path.sh", "ap-host-native-window.sh", "ap-collector-readiness.sh"} {
		libs += `. "` + harness.Path(t, "scripts/deploy/lib/"+name) + "\"\n"
	}

	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create case: %v", err)
	}

	script := `. "` + e.helper + "\"\n" + libs + e.endpointLibs + e.functions + cutoverWindowScript + scenario

	return dir, harness.Bash(t, harness.Cmd{Script: script, Args: []string{"W3", "5433", fault}, Env: []string{"CASE=" + dir}})
}

func listDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	return entries
}

func lines(t *testing.T, path string) []string {
	t.Helper()

	if !exists(path) {
		return nil
	}

	return strings.Split(strings.TrimSpace(harness.Read(t, path)), "\n")
}

func requirePermanentDropIns(t *testing.T, dir string) {
	t.Helper()

	for _, unit := range []string{issuerSocket, issuerService, collectorService} {
		entries := listDir(t, filepath.Join(dir, "system", unit+".d"))
		if len(entries) != 1 || entries[0].Name() != "90-native-window-permanent.conf" {
			t.Fatalf("%s drop-ins: %v", unit, entries)
		}

		info, err := os.Lstat(filepath.Join(dir, "system", unit+".d", entries[0].Name()))
		if err != nil {
			t.Fatalf("stat %s drop-in: %v", unit, err)
		}

		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			t.Fatalf("%s permanent drop-in must be a 0644 regular file: %v", unit, info.Mode())
		}
	}
}

func TestApplyPreflightAcceptsOnlyTheRecordedOrigin(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		name, mutation string
		valid          bool
	}{
		{"same", "", true},
		{"other-family", "native_release_window=W9", false},
		{"unaccepted", `rm "$old_target/accepted-window"`, false},
		{"no-current", `rm "$current_link"`, false},
		{"guard", `mkdir "$native_recovery_dir"`, false},
		{"broken-guard", `ln -s missing "$native_recovery_dir"`, false},
		{"held-window", `mkdir "$old_target/native-window"`, false},
		{"copied", `cp -a "$old_target" "$releases_root/other"; ln -sfn "$releases_root/other" "$current_link"`, false},
		{"recreated", `mv "$old_target" "$releases_root/moved"; cp -a "$releases_root/moved" "$old_target"`, false},
		{"copied-file", `cp -p "$old_target/accepted-window" "$old_target/copy"; mv -T "$old_target/copy" "$old_target/accepted-window"`, false},
		{"old-revision", `echo "$EXPECTED_REVISION" > "$old_target/po-sandbox/revision"`, false},
		{"writable", `chmod 0666 "$old_target/accepted-window"`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			_, r := e.windowCase(t, normalMode, cutoverApplyOriginScenario+c.mutation+"\nnative_apply_preflight\n")
			if (r.ExitCode == 0) != c.valid {
				t.Fatalf("valid=%t: %s", c.valid, r)
			}

			if c.valid && !slices.Equal(strings.Fields(r.Stdout)[:2], []string{"old", strings.Repeat("0", 39) + "1"}) {
				t.Fatalf("origin pin: %q", r.Stdout)
			}
		})
	}
}

func TestApplyOriginDispatchRejectsDrift(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, fault := range []string{normalMode, "env", "collector", "issuer", "socket", "profile", "infra", "inputs", "used-stage", "used-release", "drop-dir", "prior-guard", "need-reload"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, fault, cutoverOrdinaryOrigin+e.originDispatch)
			if (r.ExitCode == 0) != (fault == normalMode) {
				t.Fatalf("%s", r)
			}

			if fault == "need-reload" && !strings.Contains(r.Stderr, "NeedDaemonReload") {
				t.Fatalf("a pending daemon-reload must be named: %s", r.Stderr)
			}

			if fault == "prior-guard" {
				entries := lines(t, filepath.Join(dir, "recovery/claim"))
				if !slices.Equal(entries, []string{"preserved"}) || len(listDir(t, filepath.Join(dir, "recovery"))) != 1 {
					t.Fatal("a prior guard must be left exactly as found")
				}
			} else if exists(filepath.Join(dir, "recovery")) {
				t.Fatal("origin inspection must not claim a guard")
			}

			if !resolvesTo(t, filepath.Join(dir, "current"), filepath.Join(dir, "releases/old")) || exists(filepath.Join(dir, "calls")) {
				t.Fatal("origin inspection must not change the runtime")
			}
		})
	}
}

func TestApplyRefusesToStopWithoutItsOwnGuard(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for name, mutation := range map[string]string{
		"replaced":   `rm "$old_target/accepted-window"; mv "$old_target/seed-guard" "$old_target/first-guard"; mv "$native_recovery_dir" "$PWD/claim"; seed_accepted "$native_release_window"; mv "$PWD/claim" "$native_recovery_dir"`,
		"guard":      `echo foreign > "$native_recovery_dir/claim"`,
		"incomplete": `touch "$native_recovery_dir/outcome"`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, normalMode, cutoverApplyOrigin+mutation+"\nnative_apply_before_stop\ntouch stopped\n")
			if r.ExitCode == 0 || exists(filepath.Join(dir, "stopped")) {
				t.Fatalf("%s", r)
			}
		})
	}
}

func TestSameWindowStopFailuresLeaveTheOriginInPlace(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)
	faults := make([]string, 0, 11)

	faults = append(faults, "stop_client_error", "no_drain")

	for _, name := range []string{issuerSocket, issuerService, collectorService} {
		for _, prefix := range []string{"queued-", "query-", "missing-receipt-"} {
			faults = append(faults, prefix+name)
		}
	}

	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, fault, ordinaryScenario)
			if r.ExitCode == 0 || harness.Read(t, filepath.Join(dir, "recovery/outcome")) != "outcome_unknown\n" {
				t.Fatalf("%s", r)
			}

			if harness.Read(t, filepath.Join(dir, "recovery/step")) != "stop\n" || exists(filepath.Join(dir, "starts")) {
				t.Fatal("a failed stop must not start anything")
			}

			if !resolvesTo(t, filepath.Join(dir, "current"), filepath.Join(dir, "releases/old")) || exists(filepath.Join(dir, "previous")) ||
				exists(filepath.Join(dir, "releases/new/native-observation/original-stops")) {
				t.Fatal("a failed stop must leave the origin and its pointers untouched")
			}
		})
	}
}

func TestSameWindowRestoreStartsOnlyAVerifiedOrigin(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		name, mutation, fault string
		starts                []string
	}{
		{"verified", "", normalMode, []string{issuerSocket, issuerService, collectorService}},
		{"failed-first-restore-start", "", "restore_lost", []string{issuerSocket}},
		{"post-stop-job", "touch job-pending", normalMode, nil},
		{"replaced-origin", `rm "$old_target/accepted-window"`, normalMode, nil},
		{"tampered-stop", `echo changed > "$window/$po_service.stopped"`, normalMode, nil},
		{"candidate-request", `mkdir "$window/$po_socket.requesting"`, normalMode, nil},
		{"changed-config", `echo foreign >> "$host_env"`, normalMode, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, c.fault, ordinaryStoppedScenario+c.mutation+"\nnative_cutover_run failure bash -c \"exit 7\"\n")
			if r.ExitCode != 7 {
				t.Fatalf("%s", r)
			}

			if got := lines(t, filepath.Join(dir, "starts")); !slices.Equal(got, c.starts) {
				t.Fatalf("starts=%q, want %q: %s", got, c.starts, r.Stderr)
			}

			if c.name == "verified" {
				if exists(filepath.Join(dir, "recovery")) || !resolvesTo(t, filepath.Join(dir, "current"), filepath.Join(dir, "releases/old")) ||
					harness.Read(t, filepath.Join(dir, "collector-host.env")) != harness.Read(t, filepath.Join(dir, "releases/old/collector-host.env")) {
					t.Fatal("a verified restore must return the origin and release the guard")
				}

				return
			}

			if harness.Read(t, filepath.Join(dir, "recovery/outcome")) != "outcome_unknown\n" {
				t.Fatal("an unverifiable restore must record outcome_unknown")
			}

			if c.starts == nil && (harness.Read(t, filepath.Join(dir, "recovery/step")) != "failure\n" || !resolvesTo(t, filepath.Join(dir, "current"), filepath.Join(dir, "releases/new"))) {
				t.Fatal("a refused restore must leave the failed step and candidate pointer as evidence")
			}
		})
	}
}

func TestApplyAcceptanceRequiresTheObservedCandidate(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for _, c := range []struct {
		name, mutation, fault string
		accepted              bool
	}{
		{normalMode, "", normalMode, true},
		{"issuer-rotation", "retire_issuer rotate", normalMode, true},
		{"issuer-failed", "retire_issuer rotate 23", normalMode, false},
		{"issuer-exits-under-fence", issuerExitsUnderFence, normalMode, true},
		{"issuer-exits-after-commit", issuerExitsAfterCommit, normalMode, true},
		{"issuer-exits-during-fence-probe", issuerExitsDuringFenceProbe, normalMode, true},
		{"issuer-skipped-by-exec-condition", issuerSkippedByExecCondition, normalMode, true},
		{"socket-failed-without-trip", socketFailsWithoutTrip, normalMode, false},
		{"permit-expired", `rmdir "$(native_window_permit rotation)"`, normalMode, false},
		{"unobserved", `rm "$window/await-observation"`, normalMode, false},
		{"foreign-nonce", `window_nonce=$(printf "%036d" 7)`, normalMode, false},
		{"new-release-old-nonce", `release_id=other; release_dir="$releases_root/other"; cp -a "$releases_root/new" "$release_dir"; ln -sfn "$release_dir" "$current_link"; window="$release_dir/native-observation"`, normalMode, false},
		{"different-generation", `printf "%032d\n" 11 > "live-$po_socket"`, normalMode, false},
		{"other-family", "native_release_window=W9", normalMode, false},
		{"missing-start", `rm "$window/$unit.started"`, normalMode, false},
		{"missing-journal", "", "nojournal", false},
		{"issuer-load-failure", "", "load_failure", false},
		{"competing-start", "", "competing_start", false},
		{"foreign-start", "", "foreign_start", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, c.fault, applyStageScenario+c.mutation+"\nnative_apply_close_observation\nnative_apply_preflight new\n")
			if (r.ExitCode == 0) != c.accepted {
				t.Fatalf("accepted=%t: %s", c.accepted, r)
			}

			if exists(filepath.Join(dir, "current/accepted-window")) != c.accepted || exists(filepath.Join(dir, "recovery")) == c.accepted {
				t.Fatal("only an accepted candidate may carry the marker, and only it releases the guard")
			}

			if c.accepted {
				requirePermanentDropIns(t, dir)
				requireServingIssuer(t, dir)
			}

			if c.fault == "load_failure" {
				if exists(filepath.Join(dir, "releases/new/native-observation/issuer.socket.requesting")) {
					t.Fatal("a load failure must not leave a start request")
				}

				for _, unit := range []string{issuerSocket, issuerService, collectorService} {
					if !exists(filepath.Join(dir, "down-"+unit)) {
						t.Fatalf("%s must be down after a load failure", unit)
					}
				}
			}
		})
	}
}

func requireServingIssuer(t *testing.T, dir string) {
	t.Helper()

	for _, unit := range []string{issuerSocket, issuerService} {
		if exists(filepath.Join(dir, "failed-"+unit)) || exists(filepath.Join(dir, "down-"+unit)) {
			t.Fatalf("%s must be serving after acceptance", unit)
		}
	}
}

const socketActivation = `
eval "fixture_$(declare -f po_health_ready)"
po_health_ready() {
  if [[ ! -e "$native_recovery_dir/accepting" && ! -e "failed-$po_socket" && ! -e "down-$po_socket" && -e "down-$po_service" ]]; then
    rm "down-$po_service"
    printf '%032d\n' 301 > "live-$po_service"
  fi
  fixture_po_health_ready "$@"
}
trip_issuer_socket() {
  retire_issuer stop
  touch "failed-$po_socket" "tripped-$po_socket" "down-$po_socket"
}
`

const issuerExitsUnderFence = socketActivation + `
eval "fixture_$(declare -f native_window_commit_policy)"
native_window_commit_policy() {
  trip_issuer_socket
  fixture_native_window_commit_policy "$@"
}
`

const issuerExitsAfterCommit = socketActivation + `
eval "fixture_$(declare -f native_window_barriers)"
native_window_barriers() {
  fixture_native_window_barriers "$@" || return
  if [[ "$1" == finish ]]; then trip_issuer_socket; fi
}
`

const issuerExitsDuringFenceProbe = socketActivation + `
eval "fixture_$(declare -f native_window_probe_units)"
native_window_probe_units() {
  if [[ "${FUNCNAME[1]}" == native_window_fence_probe && -e "$native_recovery_dir/accepting" && ! -e probe-tripped ]]; then
    touch probe-tripped
    trip_issuer_socket
    return 1
  fi
  fixture_native_window_probe_units "$@"
}
`

const issuerSkippedByExecCondition = issuerExitsAfterCommit + `
eval "fixture_$(declare -f native_window_prop)"
native_window_prop() {
  if [[ "$1" == "$po_service" && "$2" == Result && -e "down-$po_service" ]]; then echo exec-condition; return; fi
  fixture_native_window_prop "$@"
}
`

const socketFailsWithoutTrip = socketActivation + `
eval "fixture_$(declare -f native_window_commit_policy)"
native_window_commit_policy() {
  retire_issuer stop
  touch "failed-$po_socket" "down-$po_socket"
  fixture_native_window_commit_policy "$@"
}
`

const seedLegacyDropIns = `
mkdir -p "$PWD/legacy-rel1/permanent" "$PWD/legacy-rel2/permanent"
for n in "$po_socket" "$po_service" "$unit"; do
  mkdir -p -m 0755 "$PWD/system/$n.d"
  native_window_barrier_body "$n" permanent > "$PWD/legacy-rel1/permanent/$n.conf"
  ln -sT "$PWD/legacy-rel1/permanent/$n.conf" "$PWD/system/$n.d/90-native-window-11111111-1111-1111-1111-111111111111.conf"
done
native_window_barrier_body "$po_service" permanent > "$PWD/legacy-rel2/permanent/$po_service.conf"
ln -sT "$PWD/legacy-rel2/permanent/$po_service.conf" "$PWD/system/$po_service.d/90-native-window-22222222-2222-2222-2222-222222222222.conf"
`

const seedExistingPermanent = `
for n in "$po_socket" "$po_service" "$unit"; do
  mkdir -p -m 0755 "$PWD/system/$n.d"
  native_window_barrier_body "$n" permanent > "$PWD/system/$n.d/90-native-window-permanent.conf"
  chmod 0644 "$PWD/system/$n.d/90-native-window-permanent.conf"
done
`

func TestApplyAcceptanceLeavesOnlyThePermanentDropIn(t *testing.T) {
	t.Parallel()

	e := newCutoverEnv(t)

	for name, seed := range map[string]string{
		"consolidates-legacy-symlinks": seedLegacyDropIns,
		"redeploy-is-idempotent":       seedExistingPermanent,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir, r := e.windowCase(t, normalMode, seed+applyStageScenario+"\nnative_apply_close_observation\nnative_apply_preflight new\n")
			if r.ExitCode != 0 {
				t.Fatalf("%s", r)
			}

			requirePermanentDropIns(t, dir)
		})
	}
}

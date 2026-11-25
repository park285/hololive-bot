package prestart

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	apiUnit   = "hololive-api"
	alarmUnit = "hololive-alarm-worker"
)

const fakeSystemctl = `case "$*" in
  show-environment)
    [[ ! -f "$HOLOLIVE_QUADLET_TEST_ROOT/manager-environment" ]] || cat "$HOLOLIVE_QUADLET_TEST_ROOT/manager-environment" ;;
  'show --property=UnitPath --value')
    printf '%s\n' "$HOLOLIVE_QUADLET_TEST_ROOT/etc/systemd/system" "$HOLOLIVE_QUADLET_TEST_ROOT/custom-systemd" ;;
  show\ *.service\ --property=NeedDaemonReload\ --value)
    if [[ -e "$HOLOLIVE_QUADLET_TEST_ROOT/manager-reload-required" ]]; then echo yes; else echo no; fi ;;
  show\ *.service\ --property=Names\ --value) printf '%s\n' "$2" ;;
  show\ *.service\ --property=LoadState\ --value) echo loaded ;;
  show\ *.service\ --property=FragmentPath\ --value) echo "/run/systemd/generator/$2" ;;
esac
exit 0
`

const fakeIP = `if [[ "$1" == -6 ]]; then
  printf '5: tailscale0    inet6 fd7a:115c:a1e0::8/128 scope global\n'
else
  printf '5: tailscale0    inet 100.100.1.8/32 scope global tailscale0\n'
fi
`

type collectorSlot struct {
	unit, passwordKey string
}

func central(t *testing.T) collectorSlot {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.sh"
table="$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.tsv"
slot="$(collector_slots_where host central "$table")"
unit="$(collector_slot "$slot" collector_unit "$table")"
printf '%s\nHOLOLIVE_COLLECTOR_%s_PASSWORD\n' "${unit%.service}" "${slot^^}"
`})
	harness.Succeeds(t, "central collector slot", r)

	fields := strings.Fields(r.Stdout)
	if len(fields) != 2 {
		t.Fatalf("central collector slot: %q", r.Stdout)
	}

	return collectorSlot{unit: fields[0], passwordKey: fields[1]}
}

type host struct {
	root    string
	compose []string
	slot    collectorSlot
}

func newHost(t *testing.T) *host {
	t.Helper()

	h := &host{root: t.TempDir(), slot: central(t)}
	h.copyGlob(t, "deploy/quadlet/hololive-bot/*.container")
	copyInto(t, "scripts/ci/postgres-capacity-policy.tsv", h.path("deploy/scripts/ci/postgres-capacity-policy.tsv"))
	copyInto(t, "scripts/systemd/admin-dashboard-ingress.nft", h.path("deploy/scripts/systemd/admin-dashboard-ingress.nft"))
	copyInto(t, "scripts/systemd/admin-dashboard-ingress.nft", h.path("installed.nft"))
	copyInto(t, "deploy/topology/hosts.conf", h.path("deploy/deploy/topology/hosts.conf"))
	harness.Write(t, h.path("deploy/hololive/hololive-api/VERSION"), "13.3.0\n")
	harness.Write(t, h.path("secrets/bot.env"), "API_SECRET_KEY=x\n")
	harness.Write(t, h.path("secrets/alarm-worker.env"), "FIXTURE=worker\n")
	harness.Write(t, h.path("secrets/collector.env"), "FIXTURE=collector\n")

	if err := os.MkdirAll(h.path("secrets/certs"), 0o700); err != nil {
		t.Fatal(err)
	}

	harness.Executable(t, h.path("bin/tailscale"), `[[ "$1 $2" == 'ip --assert' && ( "$3" == 100.100.1.8 || "$3" == fd7a:115c:a1e0::8 ) ]]`+"\n")
	harness.Executable(t, h.path("bin/systemctl"), fakeSystemctl)
	harness.Executable(t, h.path("bin/nft"), "cat "+harness.Path(t, "scripts/deploy/testdata/ingress-firewall.json")+"\n")
	harness.Executable(t, h.path("bin/ip"), fakeIP)
	harness.Executable(t, h.path("bin/podman"), "exit 2\n")

	h.compose = []string{
		"HOLOLIVE_DB_PASSWORD=runtime-secret",
		"LIVE_LOGS_PATH=/var/log/hololive",
		"VALKEY_BIND_IP=100.100.1.8",
		"HOLOLIVE_API_PORT_BIND_IP=100.100.1.8",
		"HOLOLIVE_ADMIN_API_PORT_BIND_IP=100.100.1.8",
		"HOLOLIVE_H3_SERVER_NAME=100.100.1.8",
		"HOLOLIVE_ALARM_WORKER_PASSWORD=alarm-secret",
		h.slot.passwordKey + "=collector-secret",
	}
	h.writeCompose(t)

	return h
}

func (h *host) path(rel string) string {
	return filepath.Join(h.root, filepath.FromSlash(rel))
}

func (h *host) copyGlob(t *testing.T, pattern string) {
	t.Helper()

	files, err := filepath.Glob(harness.Path(t, pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob %s: %v", pattern, err)
	}

	for _, file := range files {
		data, err := os.ReadFile(file) // #nosec G304 -- 저장소의 unit 파일만 읽는다.
		if err != nil {
			t.Fatal(err)
		}

		harness.Write(t, filepath.Join(h.path("units"), filepath.Base(file)), string(data))
	}
}

func (h *host) set(t *testing.T, key, value string) {
	t.Helper()

	for i, line := range h.compose {
		if strings.HasPrefix(line, key+"=") {
			h.compose[i] = key + "=" + value
			h.writeCompose(t)

			return
		}
	}

	h.compose = append(h.compose, key+"="+value)
	h.writeCompose(t)
}

func (h *host) unset(t *testing.T, key string) {
	t.Helper()

	kept := h.compose[:0]
	for _, line := range h.compose {
		if !strings.HasPrefix(line, key+"=") {
			kept = append(kept, line)
		}
	}

	h.compose = kept
	h.writeCompose(t)
}

func (h *host) writeCompose(t *testing.T) {
	t.Helper()
	harness.Write(t, h.path("compose.env"), strings.Join(h.compose, "\n")+"\n")
}

func (h *host) prestart(t *testing.T, prelude string, args ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `set -a
. "$HOLOLIVE_QUADLET_TEST_ROOT/compose.env"
set +a
` + prelude + `
exec bash "$SCRIPTTEST_ROOT/scripts/deploy/quadlet-prestart.sh" "$@"
`,
		Args: args,
		Env:  []string{"HOLOLIVE_QUADLET_TEST_ROOT=" + h.root},
		Bin:  h.path("bin"),
	})
}

func (h *host) start(t *testing.T, service string) harness.Result {
	t.Helper()

	return h.prestart(t, "", service)
}

func (h *host) envFile(service string) string {
	return h.path("run/quadlet-env/" + service + ".env")
}

func (h *host) apps() []string {
	return []string{apiUnit, alarmUnit, h.slot.unit}
}

func (h *host) rejectsBeforeSecrets(t *testing.T, label, want, service, prelude string) {
	t.Helper()

	if err := os.Remove(h.envFile(service)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}

	r := h.prestart(t, prelude, service)
	harness.Fails(t, label, r)

	if want != "" && !strings.Contains(r.Stdout+r.Stderr, want) {
		t.Fatalf("%s: output missing %q:\n%s", label, want, r)
	}

	if _, err := os.Stat(h.envFile(service)); err == nil {
		t.Fatalf("%s: secrets were written before the rejection", label)
	}
}

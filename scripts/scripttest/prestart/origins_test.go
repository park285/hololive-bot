package prestart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const unsupportedOrigin = "unsupported Quadlet/systemd override origin:"

func TestPrestartRejectsAWildcardAdminBindBeforeWritingSecrets(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	h.set(t, "HOLOLIVE_ADMIN_API_PORT_BIND_IP", "0.0.0.0")
	h.rejectsBeforeSecrets(t, "wildcard admin bind", "", apiUnit, "")
}

func TestPrestartWritesPrivateContainerEnvFiles(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	for _, service := range h.apps() {
		harness.Succeeds(t, service, h.start(t, service))

		if harness.Size(t, h.envFile(service)) == 0 {
			t.Errorf("%s env file is empty", service)
		}
	}

	env := harness.Read(t, h.envFile(apiUnit))

	for _, want := range []string{"POSTGRES_PASSWORD=runtime-secret\n", "APP_VERSION=13.3.0\n"} {
		if !strings.Contains(env, want) {
			t.Errorf("api env missing %q", want)
		}
	}

	if mode := harness.Mode(t, h.envFile(apiUnit)).Perm(); mode != 0o600 {
		t.Errorf("api env mode = %o", mode)
	}
}

var unsupportedOrigins = []string{
	"units/container.d/60-pool.conf",
	"units/hololive-.container.d/60-pool.conf",
	"units/hololive-collector-.container.d/60-pool.conf",
	"etc/containers/systemd/container.d/60-pool.conf",
	"run/containers/systemd/hololive-.container.d/60-pool.conf",
	"run/containers/systemd/users/hololive-.container.d/60-pool.conf",
	"usr/share/containers/systemd/nested/hololive-api.container.d/60-pool.conf",
	"run/containers/systemd/nested/hololive-api.container",
	"etc/systemd/system/hololive-api.service.d/60-pool.conf",
	"run/systemd/system/hololive-.service.d/60-pool.conf",
	"usr/lib/systemd/system/service.d/60-pool.conf",
	"run/systemd/generator/hololive-api.service.d/60-pool.conf",
	"run/systemd/generator.early/hololive-api.service",
	"etc/systemd/system/hololive-api.service",
	"custom-systemd/hololive-api.service.d/60-pool.conf",
}

func poolOverride(origin string) string {
	section := "Container"

	if strings.Contains(origin, "/systemd/system/") || strings.Contains(origin, "/systemd/generator") || strings.HasPrefix(origin, "custom-systemd/") {
		section = "Service"
	}

	return "[" + section + "]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n"
}

func TestPrestartRejectsUnknownOverrideOrigins(t *testing.T) {
	t.Parallel()

	h := newHost(t)

	for _, origin := range unsupportedOrigins {
		harness.Write(t, h.path(origin), poolOverride(origin))

		for _, service := range h.apps() {
			h.rejectsBeforeSecrets(t, origin+" ("+service+")", unsupportedOrigin, service, "")
		}

		harness.Remove(t, h.path(origin))
	}

	for _, key := range []string{"QUADLET_UNIT_DIRS", "SYSTEMD_UNIT_PATH"} {
		h.rejectsBeforeSecrets(t, key, unsupportedOrigin, apiUnit, "export "+key+"="+h.path("units"))
	}

	for _, state := range []string{"manager-environment", "manager-reload-required"} {
		harness.Write(t, h.path(state), "QUADLET_UNIT_DIRS=/unreviewed\n")
		h.rejectsBeforeSecrets(t, state, unsupportedOrigin, apiUnit, "")
		harness.Remove(t, h.path(state))
	}
}

func TestPrestartExactDropInKeepsComposePrecedence(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	harness.Write(t, h.path("units/hololive-api.container.d/60-pool.conf"), "[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
	h.set(t, "ADMIN_API_POSTGRES_POOL_MAX_CONNS", "4")

	harness.Succeeds(t, "exact drop-in", h.start(t, apiUnit))

	if harness.Size(t, h.envFile(apiUnit)) == 0 {
		t.Fatal("api env file is empty")
	}
}

var neutralOrigins = []string{
	"units/container.d/71-security.conf",
	"units/hololive-.container.d/72-security.conf",
	"etc/containers/systemd/hololive-api.container.d/73-security.conf",
	"usr/share/containers/systemd/nested/hololive-alarm-worker.container.d/74-security.conf",
}

const neutralMCS = "# host MCS\n[Container]\nSecurityLabelLevel=s0:c900,c901\n"

func TestPrestartAcceptsOnlyPureSecurityOrigins(t *testing.T) {
	t.Parallel()

	h := newHost(t)

	for _, origin := range neutralOrigins {
		harness.Write(t, h.path(origin), neutralMCS)
		harness.Succeeds(t, origin, h.start(t, apiUnit))
		harness.Write(t, h.path(origin), neutralMCS+"Environment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
		h.rejectsBeforeSecrets(t, origin+" mixed", unsupportedOrigin, apiUnit, "")
		harness.Remove(t, h.path(origin))
	}

	origin := neutralOrigins[2]

	for _, body := range []string{
		"[Service]\nSecurityLabelLevel=s0:c900,c901\n",
		"[Container]\nSecurityLabelLevel=\"s0:c900,c901\"\n",
		"[Container]\nSecurityLabelLevel=s0:c900,\\\nc901\n",
		"[Container]\nSecurityLabelLevel=s0:c900,c901\n[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n",
		"[Container]\nSecurityLabelLevel=s0:c900,c901\nSecurityLabelLevel=s0:c902,c903\n",
		"[Container]\nSecurityLabelLevel=s0:c900,c901\nSecurityLabelDisable=true\n",
	} {
		harness.Write(t, h.path(origin), body)
		h.rejectsBeforeSecrets(t, body, unsupportedOrigin, apiUnit, "")
	}

	harness.Write(t, h.path(origin), neutralMCS)
	harness.Write(t, h.path("units/hololive-api.container.d/"+filepath.Base(origin)), "[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n")
	h.rejectsBeforeSecrets(t, "same-name capacity drop-in", unsupportedOrigin, apiUnit, "")
}

func TestPrestartKeepsTheStaticConnectionBudget(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	h.set(t, "BOT_POSTGRES_POOL_MAX_CONNS", "40")
	h.rejectsBeforeSecrets(t, "exhausted budget", "", apiUnit, "")
	h.unset(t, "BOT_POSTGRES_POOL_MAX_CONNS")
	harness.Succeeds(t, "within budget", h.start(t, apiUnit))
}

func TestPrestartWaitsForAnIPv6AdminBind(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	h.set(t, "HOLOLIVE_ADMIN_API_PORT_BIND_IP", "fd7a:115c:a1e0::8")

	r := h.prestart(t, "exec() { timeout 20 \"$@\"; }", apiUnit)
	harness.Succeeds(t, "IPv6 admin bind", r)

	if harness.Size(t, h.envFile(apiUnit)) == 0 {
		t.Fatal("api env file is empty")
	}
}

func TestPrestartRejectsDropInsOnGeneratedServices(t *testing.T) {
	t.Parallel()

	const quadlet = "/usr/libexec/podman/quadlet"

	if _, err := os.Stat(quadlet); err != nil {
		t.Skip("podman quadlet generator is not installed")
	}

	h := newHost(t)
	h.copyGlob(t, "deploy/quadlet/hololive-bot/*.network")
	h.copyGlob(t, "deploy/quadlet/hololive-bot/*.volume")

	generated := h.path("run/systemd/generator")
	cmd := exec.CommandContext(t.Context(), quadlet, generated) // #nosec G204 -- 시스템 quadlet 생성기를 fixture 디렉터리에 실행한다.

	cmd.Env = append(os.Environ(), "QUADLET_UNIT_DIRS="+h.path("units"))

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("quadlet generator: %v\n%s", err, out)
	}

	harness.Succeeds(t, "generated units", h.start(t, apiUnit))
	harness.Write(t, filepath.Join(generated, "hololive-api.service.d/60-pool.conf"), "[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
	h.rejectsBeforeSecrets(t, "generated service drop-in", unsupportedOrigin, apiUnit, "")
	harness.Remove(t, filepath.Join(generated, "hololive-api.service.d/60-pool.conf"))
	harness.Succeeds(t, "generated units again", h.start(t, apiUnit))
}

func TestValkeyPrestartPublishesOnlyOnTheTailnet(t *testing.T) {
	t.Parallel()

	h := newHost(t)
	h.set(t, "VALKEY_BIND_IP", "0.0.0.0")

	r := h.start(t, "valkey-cache")
	harness.Fails(t, "wildcard Valkey bind", r)

	if !strings.Contains(r.Stdout+r.Stderr, "Valkey bind must match the topology primary") {
		t.Fatalf("valkey rejection:\n%s", r)
	}

	h.set(t, "VALKEY_BIND_IP", "100.100.1.8")
	harness.Succeeds(t, "tailnet Valkey bind", h.start(t, "valkey-cache"))
}

package prestart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

var syntheticSecrets = []string{"runtime-secret", "alarm-secret", "collector-secret", "synthetic-holodex", "injected-secret"}

type renderHost struct {
	*host

	normal map[string]string
}

func newRenderHost(t *testing.T) renderHost {
	t.Helper()

	h := newHost(t)
	harness.Write(t, h.path("secrets/alarm-worker.env"), "WORKER_SECRET=synthetic\n")
	harness.Write(t, h.path("secrets/collector.env"), "COLLECTOR_SECRET=synthetic\n")
	h.set(t, "HOLODEX_API_KEY", "synthetic-holodex")
	h.set(t, "HOLOLIVE_METRICS_PORT_BIND_IP", "100.100.1.8")

	r := renderHost{host: h, normal: map[string]string{}}
	for _, unit := range h.apps() {
		harness.Succeeds(t, unit, h.start(t, unit))

		r.normal[unit] = harness.Read(t, h.envFile(unit))
	}

	if err := os.Rename(h.path("bin"), h.path("online-bin")); err != nil {
		t.Fatal(err)
	}

	for _, command := range []string{"ip", "tailscale", "nft", "systemctl", "podman", "docker", "sleep", "install"} {
		harness.Executable(t, h.path("bin/"+command), `printf '%s\n' "${0##*/}" >> "$HOLOLIVE_QUADLET_TEST_ROOT/unexpected-operations"
exit 93
`)
	}

	return r
}

func privateDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "out")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	return dir
}

func entries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()

	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	return list
}

func (r renderHost) render(t *testing.T, prelude string, args ...string) harness.Result {
	t.Helper()

	return r.prestart(t, prelude, append([]string{"--render-env"}, args...)...)
}

func (r renderHost) assertNoHostOperations(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(r.path("unexpected-operations")); err == nil {
		t.Fatalf("render touched host commands: %s", harness.Read(t, r.path("unexpected-operations")))
	}
}

func (r renderHost) assertPrivateOutput(t *testing.T, res harness.Result, dir, unit string) {
	t.Helper()

	harness.Succeeds(t, "render "+unit, res)

	if res.Stdout+res.Stderr != "" {
		t.Errorf("render %s printed diagnostics:\n%s", unit, res)
	}

	if mode := harness.Mode(t, dir).Perm(); mode != 0o700 {
		t.Errorf("output dir mode = %o", mode)
	}

	file := filepath.Join(dir, unit+".env")
	if mode := harness.Mode(t, file).Perm(); mode != 0o600 {
		t.Errorf("%s mode = %o", file, mode)
	}

	if got := harness.Read(t, file); got != r.normal[unit] {
		t.Errorf("%s differs from the normal prestart output:\n%s\nwant:\n%s", unit, got, r.normal[unit])
	}

	if n := len(entries(t, dir)); n != 1 {
		t.Errorf("output dir holds %d entries", n)
	}

	r.assertNoHostOperations(t)
}

func (r renderHost) assertRejected(t *testing.T, label string, res harness.Result, dir string) {
	t.Helper()

	harness.Fails(t, label, res)

	if info, err := os.Stat(dir); err == nil && info.IsDir() && len(entries(t, dir)) != 0 {
		t.Errorf("%s: rejected render left files in %s", label, dir)
	}

	r.assertNoHostOperations(t)

	for _, secret := range syntheticSecrets {
		if strings.Contains(res.Stdout+res.Stderr, secret) {
			t.Errorf("%s: diagnostics expose %s", label, secret)
		}
	}
}

func (r renderHost) rejects(t *testing.T, label, want string, res harness.Result, dir string) {
	t.Helper()

	r.assertRejected(t, label, res, dir)

	if want != "" && !strings.Contains(res.Stdout+res.Stderr, want) {
		t.Errorf("%s: diagnostics missing %q:\n%s", label, want, res)
	}
}

func TestRenderEnvMatchesPrestartWithoutTouchingRuntimeState(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	for _, unit := range r.apps() {
		harness.Write(t, r.envFile(unit), "POSTGRES_PASSWORD=stale-secret\n")

		dir := privateDir(t)
		r.assertPrivateOutput(t, r.render(t, "", unit, dir), dir, unit)

		if got := harness.Read(t, r.envFile(unit)); got != "POSTGRES_PASSWORD=stale-secret\n" {
			t.Errorf("render replaced the runtime env of %s", unit)
		}
	}

	if err := os.Rename(r.path("run"), r.path("previous-run")); err != nil {
		t.Fatal(err)
	}

	for _, unit := range r.apps() {
		dir := privateDir(t)
		r.assertPrivateOutput(t, r.render(t, "", unit, dir), dir, unit)

		if _, err := os.Stat(r.path("run")); err == nil {
			t.Fatal("render created the runtime directory")
		}
	}
}

func TestRenderEnvUsesTheServiceInput(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	dir := privateDir(t)
	res := r.render(t, "export HOLOLIVE_DB_PASSWORD=service-override\nunset HOLODEX_API_KEY", apiUnit, dir)

	harness.Succeeds(t, "service input", res)

	if res.Stdout+res.Stderr != "" {
		t.Errorf("render printed diagnostics:\n%s", res)
	}

	env := harness.Read(t, filepath.Join(dir, "hololive-api.env"))

	for _, want := range []string{"POSTGRES_PASSWORD=service-override\n", "HOLODEX_API_KEY=\n"} {
		if !strings.Contains(env, want) {
			t.Errorf("rendered env missing %q", want)
		}
	}
}

func TestRenderEnvRejectsMissingOrMultilineInputs(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	required := map[string][]string{
		apiUnit:     {"HOLOLIVE_DB_PASSWORD", "LIVE_LOGS_PATH", "VALKEY_BIND_IP", "HOLOLIVE_API_PORT_BIND_IP", "HOLOLIVE_ADMIN_API_PORT_BIND_IP", "HOLOLIVE_H3_SERVER_NAME"},
		alarmUnit:   {"HOLOLIVE_ALARM_WORKER_PASSWORD", "LIVE_LOGS_PATH", "VALKEY_BIND_IP"},
		r.slot.unit: {r.slot.passwordKey, "LIVE_LOGS_PATH"},
	}

	for unit, keys := range required {
		for _, key := range keys {
			dir := privateDir(t)
			r.rejects(t, unit+" without "+key, key, r.render(t, "unset "+key, unit, dir), dir)
		}

		dir := privateDir(t)
		r.rejects(t, unit+" multiline", "", r.render(t, "export HOLODEX_API_KEY=$'injected-secret\\nSECOND=value'", unit, dir), dir)
	}
}

func TestRenderEnvRejectsInvalidEnvFiles(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	valid := append([]string(nil), r.compose...)

	for _, line := range []string{"INVALID", "BAD KEY=value", "LIVE_LOGS_PATH=duplicate", "SPACE= value", "SPACE=value ", "COMMAND=$(id)", "COMMAND=`id`", "CONTROL=a\x01b"} {
		harness.Write(t, r.path("compose.env"), strings.Join(append(append([]string(nil), valid...), line), "\n")+"\n")

		dir := privateDir(t)
		res := harness.Bash(t, harness.Cmd{
			Script: `exec bash "$SCRIPTTEST_ROOT/scripts/deploy/quadlet-prestart.sh" --render-env hololive-api "$1"`,
			Args:   []string{dir},
			Env:    []string{"HOLOLIVE_QUADLET_TEST_ROOT=" + r.root},
			Bin:    r.path("bin"),
		})
		r.rejects(t, "compose line "+line, "compose env file is invalid", res, dir)
	}

	r.writeCompose(t)

	for unit, file := range map[string]string{apiUnit: "bot.env", alarmUnit: "alarm-worker.env", r.slot.unit: "collector.env"} {
		path := r.path("secrets/" + file)
		original := harness.Read(t, path)
		harness.Write(t, path, "INVALID\n")

		dir := privateDir(t)
		r.rejects(t, unit+" container env", "container env file is invalid", r.render(t, "", unit, dir), dir)
		harness.Write(t, path, original)
	}
}

func TestRenderEnvValidatesTheAppVersion(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	version := r.path("deploy/hololive/hololive-api/VERSION")
	harness.Remove(t, version)

	dir := privateDir(t)
	r.rejects(t, "missing VERSION", "VERSION is missing", r.render(t, "", apiUnit, dir), dir)

	dir = privateDir(t)

	res := r.render(t, "", apiUnit, dir, "--app-version", "13.2.0-rc.1+retained")

	harness.Succeeds(t, "retained version", res)

	if !strings.Contains(harness.Read(t, filepath.Join(dir, "hololive-api.env")), "APP_VERSION=13.2.0-rc.1+retained\n") {
		t.Error("retained version was not rendered")
	}

	harness.Write(t, version, "13.3.0\n")

	for _, bad := range []string{"", "13.2.0%unsafe", "13.2.0 unsafe", "13.2.0\nOTHER=value"} {
		dir := privateDir(t)
		r.assertRejected(t, "version "+bad, r.render(t, "", apiUnit, dir, "--app-version", bad), dir)
	}

	for _, unit := range []string{alarmUnit, r.slot.unit} {
		dir := privateDir(t)
		r.assertRejected(t, unit+" with app version", r.render(t, "", unit, dir, "--app-version", "13.2.0"), dir)
	}

	for _, unit := range []string{"holo-postgres", "admin-dashboard-ingress", "collector-enabled", "../hololive-api"} {
		dir := privateDir(t)
		r.assertRejected(t, "unsupported unit "+unit, r.render(t, "", unit, dir), dir)
	}
}

func TestRenderEnvRefusesUnsafeOutputDirectories(t *testing.T) {
	t.Parallel()

	r := newRenderHost(t)
	base := t.TempDir()

	if err := os.Rename(r.path("run"), r.path("previous-run")); err != nil {
		t.Fatal(err)
	}

	public := filepath.Join(base, "public")
	if err := os.Mkdir(public, 0o755); err != nil { // #nosec G301 -- 거부되어야 하는 공개 디렉터리를 만든다.
		t.Fatal(err)
	}

	r.assertRejected(t, "public output", r.render(t, "", apiUnit, public), public)

	if mode := harness.Mode(t, public).Perm(); mode != 0o755 {
		t.Errorf("public output mode changed to %o", mode)
	}

	absent := filepath.Join(base, "absent")
	r.assertRejected(t, "absent output", r.render(t, "", apiUnit, absent), absent)

	if _, err := os.Stat(absent); err == nil {
		t.Error("render created the absent output directory")
	}

	target := privateDir(t)
	link := filepath.Join(base, "link")
	harness.Symlink(t, target, link)
	r.assertRejected(t, "symlinked output", r.render(t, "", apiUnit, link), target)

	existing := privateDir(t)
	harness.Write(t, filepath.Join(existing, "hololive-api.env"), "do-not-overwrite\n")
	harness.Fails(t, "overwrite", r.render(t, "", apiUnit, existing))

	if got := harness.Read(t, filepath.Join(existing, "hololive-api.env")); got != "do-not-overwrite\n" {
		t.Error("render overwrote an existing output")
	}

	dangling := privateDir(t)
	harness.Symlink(t, filepath.Join(base, "nonexistent"), filepath.Join(dangling, "hololive-api.env"))
	harness.Fails(t, "dangling output symlink", r.render(t, "", apiUnit, dangling))

	if _, err := os.Stat(filepath.Join(base, "nonexistent")); err == nil {
		t.Error("render followed a dangling output symlink")
	}

	if _, err := os.Stat(r.path("run")); err == nil {
		t.Fatal("render created the runtime directory")
	}

	preview := r.path("run/preview")
	if err := os.MkdirAll(preview, 0o700); err != nil {
		t.Fatal(err)
	}

	r.assertRejected(t, "output inside the runtime directory", r.render(t, "", apiUnit, preview), preview)
}

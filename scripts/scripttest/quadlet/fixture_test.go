package quadlet

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	interruptedClient = "restart_client_interrupted"
	pairCandidate     = "candidate"
	pairBaseline      = "baseline"
	yes               = "true"
	no                = "false"
	epochNew          = "new"
	revision          = "0123456789abcdef0123456789abcdef01234567"
	otherRevision     = "fedcba9876543210fedcba9876543210fedcba98"
	api               = "hololive-api"
	worker            = "hololive-alarm-worker"
	apiCandidate      = "localhost/hololive-api:candidate"
	workerImage       = "localhost/hololive-alarm-worker:candidate"
	quadletBin        = "/usr/libexec/podman/quadlet"
	originDenied      = "unsupported Quadlet/systemd override origin:"
)

var (
	apiHex    = strings.Repeat("a", 64)
	workerHex = strings.Repeat("b", 64)
)

func zeros(n int) string {
	return fmt.Sprintf("%064d", n)
}

type deployCase struct {
	root, fakebin, python string
	coordinated           bool
	serverEpoch           string
}

func helper(t *testing.T) string {
	t.Helper()

	return harness.Path(t, "scripts/deploy/quadlet-redeploy-service.sh")
}

func rollback(t *testing.T) string {
	t.Helper()

	return harness.Path(t, "scripts/deploy/quadlet-rollback-service.sh")
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

func appendText(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) // #nosec G302 G304 -- fixture 경로만 연다.
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

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func read(t *testing.T, path string) string {
	t.Helper()

	return strings.TrimSuffix(harness.Read(t, path), "\n")
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()

	return harness.Read(t, a) == harness.Read(t, b)
}

func command(t *testing.T, name string, args ...string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), name, args...).CombinedOutput() // #nosec G204 -- fixture가 고정된 도구만 실행한다.
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func newCase(t *testing.T, service string, withDropin bool, apiRevision string) *deployCase {
	t.Helper()

	if os.Getuid() == 0 {
		t.Fatal("run as a non-root user; the helpers refuse the test prefix as root")
	}

	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}

	c := &deployCase{root: t.TempDir(), python: python, serverEpoch: "old"}

	c.fakebin = c.at("fakebin")

	for name, body := range fakes {
		harness.Executable(t, filepath.Join(c.fakebin, name), body)
	}

	put(t, c.at("opt/hololive-bot/compose/current/.source-revision"), revision+"\n")

	units, err := filepath.Glob(harness.Path(t, "deploy/quadlet/hololive-bot/*.container"))
	if err != nil || len(units) == 0 {
		t.Fatalf("units: %v", err)
	}

	for _, unit := range units {
		put(t, c.quadlet(filepath.Base(unit)), harness.Read(t, unit))
	}

	put(t, c.compose(), "VALKEY_BIND_IP=100.100.1.8\n")

	if withDropin {
		put(t, c.dropIn(service), "pair=baseline\n")
	}

	for _, dir := range []string{"etc/systemd/system", "state/images"} {
		if err := os.MkdirAll(c.at(dir), 0o755); err != nil { // #nosec G301 -- 호스트 디렉터리 권한을 재현한다.
			t.Fatal(err)
		}
	}

	c.addImage(t, "previous-image", zeros(8), service, revision)
	appendText(t, c.at("state/images/sha256_"+zeros(8)), "schema=text-file-v1\n")
	c.addImage(t, apiCandidate, apiHex, api, apiRevision)
	c.addImage(t, workerImage, workerHex, worker, revision)

	for name, value := range map[string]string{
		"active": yes, "pg-started": "1794960062300000000", "running-endpoint": "POSTGRES_HOST=holo-postgres\nPOSTGRES_PORT=5432",
		"nrestarts": "3", "runtime": pairBaseline,
	} {
		put(t, c.state(name), value+"\n")
	}

	put(t, c.state("calls"), "")

	return c
}

func (c *deployCase) at(rel string) string {
	return filepath.Join(c.root, filepath.FromSlash(rel))
}

func (c *deployCase) state(rel string) string {
	return c.at("state/" + rel)
}

func (c *deployCase) quadlet(rel string) string {
	return c.at("etc/containers/systemd/hololive-bot/" + rel)
}

func (c *deployCase) dropIn(service string) string {
	return c.quadlet(service + ".container.d/50-worker-profile.conf")
}

func (c *deployCase) compose() string {
	return c.at("etc/stack-secrets/hololive-bot/compose.env")
}

func (c *deployCase) current(rel string) string {
	return c.at("opt/hololive-bot/compose/current/" + rel)
}

func (c *deployCase) addImage(t *testing.T, ref, hex, title, rev string) {
	t.Helper()

	for _, name := range []string{ref, "sha256:" + hex} {
		key := strings.NewReplacer("/", "_", ":", "_").Replace(name)
		put(t, c.state("images/"+key), "id="+hex+"\nplatform=linux/arm64\ntitle="+title+"\nrevision="+rev+"\n")
	}
}

func coordinatedCase(t *testing.T) *deployCase {
	t.Helper()

	c := newCase(t, api, true, revision)

	c.coordinated = true

	for _, service := range []string{api, worker} {
		generation := 1

		if service == api {
			generation = 7
		}

		for name, value := range map[string]string{
			"active": yes, "loadstate": "loaded", "mask_scope": "generated", "conmonpid": "42", "nrestarts": "3", "runtime": pairBaseline,
			"invocation": fmt.Sprintf("%032d", generation), "cid": zeros(generation),
		} {
			put(t, c.state(service+"/"+name), value+"\n")
		}

		put(t, c.dropIn(service), "pair=baseline\n")
	}

	return c
}

func (c *deployCase) run(t *testing.T, caseName, candidateHex, tool string, args ...string) harness.Result {
	t.Helper()

	shell := shellFor(caseName)

	deployRoot, err := filepath.EvalSymlinks(c.current(""))
	if err != nil {
		t.Fatal(err)
	}

	coordinated := no
	if c.coordinated {
		coordinated = yes
	}

	return harness.Bash(t, harness.Cmd{
		Script: `exec "$@"`,
		Args:   append(append(shell, tool), args...),
		Bin:    c.fakebin,
		Env: []string{
			"HOLOLIVE_QUADLET_TEST_ROOT=" + c.root, "FAKE_STATE=" + c.at("state"), "FAKE_CASE=" + caseName,
			"FAKE_PAIR_TOOL=" + harness.Path(t, "scripts/deploy/lib/worker-profile-pair.py"),
			"FAKE_STAGE_PAIR_TOOL=" + c.at("opt/hololive-bot/compose/staged/"+revision+"/scripts/deploy/lib/worker-profile-pair.py"),
			"FAKE_REAL_PYTHON3=" + c.python, "FAKE_QUADLET_DIR=" + c.quadlet(""), "FAKE_FENCE_DIR=" + c.at("var/lib/hololive-postgres-fence"),
			"FAKE_DEPLOY_ROOT=" + filepath.Clean(deployRoot), "FAKE_CANDIDATE_ID=sha256:" + candidateHex,
			"FAKE_COORDINATED=" + coordinated, "FAKE_SERVER_EPOCH=" + c.serverEpoch,
			"FAKE_API_ID=sha256:" + apiHex, "FAKE_WORKER_ID=sha256:" + workerHex,
		},
	})
}

func shellFor(caseName string) []string {
	if caseName == interruptedClient || caseName == "term_after_first_restart" {
		return []string{"setsid", "--wait", "bash", "-c", "export FAKE_SIGNAL_OWNER=$$; exec bash \"$@\"", "bash"}
	}

	return []string{"bash"}
}

func (c *deployCase) deploy(t *testing.T, caseName string) harness.Result {
	t.Helper()

	return c.run(t, caseName, apiHex, helper(t), api, apiCandidate)
}

func (c *deployCase) quiesceWith(t *testing.T, caseName string) harness.Result {
	t.Helper()

	return c.run(t, caseName, apiHex, helper(t), "--quiesce", apiCandidate, workerImage)
}

func (c *deployCase) resume(t *testing.T, caseName, apiRelease, workerRelease string) harness.Result {
	t.Helper()

	return c.run(t, caseName, apiHex, helper(t), "--resume", apiRelease, workerRelease)
}

func (c *deployCase) rollbackRelease(t *testing.T, caseName, release, mode string) harness.Result {
	t.Helper()

	return c.run(t, caseName, apiHex, rollback(t), release, mode)
}

func succeeds(t *testing.T, label string, r harness.Result) {
	t.Helper()
	harness.Succeeds(t, label, r)
}

func fails(t *testing.T, label string, r harness.Result) {
	t.Helper()
	harness.Fails(t, label, r)
}

func (c *deployCase) calls(t *testing.T) []string {
	t.Helper()

	return strings.Split(strings.TrimSuffix(harness.Read(t, c.state("calls")), "\n"), "\n")
}

func (c *deployCase) clearCalls(t *testing.T) {
	t.Helper()
	put(t, c.state("calls"), "")
}

func (c *deployCase) assertOrder(t *testing.T, label string, expected ...string) {
	t.Helper()

	calls := c.calls(t)
	next := 0

	for _, want := range expected {
		found := -1

		for i := next; i < len(calls); i++ {
			if calls[i] == want {
				found = i

				break
			}
		}

		if found < 0 {
			t.Fatalf("%s: missing or out of order: %s\ncalls:\n%s", label, want, strings.Join(calls, "\n"))
		}

		next = found + 1
	}
}

func (c *deployCase) assertNoCall(t *testing.T, label, pattern string) {
	t.Helper()

	re := regexp.MustCompile(pattern)

	for _, call := range c.calls(t) {
		if re.MatchString(call) {
			t.Fatalf("%s: unexpected call matching %s: %s", label, pattern, call)
		}
	}
}

func (c *deployCase) countCalls(t *testing.T, line string) int {
	t.Helper()

	n := 0

	for _, call := range c.calls(t) {
		if call == line {
			n++
		}
	}

	return n
}

func (c *deployCase) installedPair(t *testing.T, service string) string {
	t.Helper()

	for line := range strings.Lines(harness.Read(t, c.dropIn(service))) {
		if value, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "pair="); ok {
			return value
		}
	}

	return ""
}

func (c *deployCase) releases(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(c.current("artifacts/worker-profile/releases"))
	if err != nil {
		t.Fatal(err)
	}

	var dirs []string

	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, c.current("artifacts/worker-profile/releases/"+entry.Name()))
		}
	}

	return dirs
}

func (c *deployCase) release(t *testing.T) string {
	t.Helper()

	dirs := c.releases(t)
	if len(dirs) != 1 {
		t.Fatalf("want one release, got %v", dirs)
	}

	return dirs[0]
}

func (c *deployCase) coordinatedRelease(t *testing.T, service string) string {
	t.Helper()

	matches, err := filepath.Glob(c.current("artifacts/worker-profile/releases/*"))
	if err != nil {
		t.Fatal(err)
	}

	for _, candidate := range matches {
		if data, err := os.ReadFile(filepath.Join(candidate, "service")); err == nil && strings.TrimSpace(string(data)) == service { // #nosec G304 -- fixture 경로만 읽는다.
			return candidate
		}
	}

	return ""
}

func (c *deployCase) assertOutput(t *testing.T, r harness.Result, pattern string) {
	t.Helper()

	if !regexp.MustCompile("(?m)" + pattern).MatchString(r.Stdout + r.Stderr) {
		t.Fatalf("output does not match %q:\n%s", pattern, r)
	}
}

func (c *deployCase) assertHeldPair(t *testing.T) {
	t.Helper()

	for _, service := range []string{api, worker} {
		if read(t, c.state(service+"/active")) != no || read(t, c.state(service+"/loadstate")) != "masked" {
			t.Fatalf("coordinated failure released the stopped peer %s", service)
		}
	}
}

func (c *deployCase) quiesce(t *testing.T) (string, string) {
	t.Helper()

	c.serverEpoch = "old"
	succeeds(t, "coordinated quiescence", c.quiesceWith(t, "success"))
	c.assertHeldPair(t)

	if exists(c.state("migrated-with")) {
		t.Fatal("quiescence ran a migration before the peer cutover")
	}

	if exists(c.state("unguarded-stop")) {
		t.Fatal("a writer stopped without a start guard or its own stop command")
	}

	return c.coordinatedRelease(t, api), c.coordinatedRelease(t, worker)
}

func (c *deployCase) assertCandidatePairRunning(t *testing.T) {
	t.Helper()

	for _, service := range []string{api, worker} {
		if read(t, c.state(service+"/active")) != yes || read(t, c.state(service+"/loadstate")) != "loaded" ||
			read(t, c.state(service+"/runtime")) != pairCandidate {
			t.Fatal("coordinated resume did not start the complete candidate pair")
		}
	}

	if exists(c.state("unsafe-start")) || exists(c.state("unsafe-migration")) {
		t.Fatal("a live old peer crossed the coordinated activation boundary")
	}

	if read(t, c.state("migrated-with")) != "sha256:"+apiHex {
		t.Fatal("coordinated resume did not use its API image for migration")
	}
}

func (c *deployCase) holdLock(t *testing.T) func() {
	t.Helper()

	path := c.at("run/lock/hololive-quadlet-redeploy.lock")
	put(t, path, "")

	lock, err := os.OpenFile(path, os.O_WRONLY, 0) // #nosec G304 -- 잠금 파일 fixture다.
	if err != nil {
		t.Fatal(err)
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil { // #nosec G115 -- 열린 파일 기술자다.
		t.Fatal(err)
	}

	return func() {
		if err := lock.Close(); err != nil {
			t.Error(err)
		}
	}
}

func quadletExecStart(t *testing.T, units, name string) (string, bool) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), quadletBin, "-dryrun") // #nosec G204 -- 시스템 quadlet 생성기다.

	cmd.Env = append(os.Environ(), "QUADLET_UNIT_DIRS="+units)

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}

	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "ExecStart=") && strings.Contains(line, " --name "+name+" ") {
			return strings.TrimSuffix(line, "\n"), true
		}
	}

	return "", false
}

func hasQuadlet() bool {
	info, err := os.Stat(quadletBin)
	if err != nil {
		return false
	}

	return info.Mode()&0o111 != 0
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	command(t, "cp", "-R", src, dst)
}

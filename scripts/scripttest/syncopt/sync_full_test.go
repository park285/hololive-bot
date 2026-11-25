package syncopt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	defaultValkeyImage = "docker.io/valkey/valkey:9.1.3-alpine3.24"
	pinnedValkeyImage  = "localhost/iris-stack-valkey:pinned"
	staleStart         = "installed Valkey start checks differ from the checkout; run a full sync first"
	unsupportedOrigin  = "unsupported Quadlet/systemd override origin"
)

func TestSyncOptRejectsAnUnadmittedValkeyStage(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")
	h := e.host(t, "host", "1")
	put(t, h.at("events.stage-denied"), "")
	e.rejects(t, h, repo, "Valkey admission", "Valkey standalone stage is not admitted")
}

func TestSyncOptFullAndValkeyOnlyPublication(t *testing.T) {
	t.Parallel()

	e := newSyncEnv(t)
	repo := e.repo(t, "repo")
	h := e.host(t, "host", "1")

	e.syncs(t, h, repo, "valid units")
	assertFullInstall(t, h, repo)
	assertValkeyOnlyLeavesOthers(t, e, h, repo)
	assertValkeyBindAndStartChecks(t, e, h, repo)
	assertValkeyImages(t, e, h, repo)
	assertValkeyStartCheckOwnership(t, e, h, repo)
	assertValkeyOverrides(t, e, h, repo)
}

func assertFullInstall(t *testing.T, h syncHost, repo string) {
	t.Helper()

	units, err := os.ReadDir(harness.Path(t, "deploy/quadlet/hololive-bot"))
	if err != nil {
		t.Fatal(err)
	}

	for _, unit := range units {
		if !sameFile(t, harness.Path(t, "deploy/quadlet/hololive-bot/"+unit.Name()), h.installed(unit.Name())) {
			t.Errorf("staged unit not installed: %s", unit.Name())
		}
	}

	if exists(h.installed("stale.container")) {
		t.Error("unit removed from the repository stayed installed")
	}

	if !strings.Contains(harness.Read(t, h.installed("hololive-api.container.d/50-worker-profile.conf")), "io.hololive.test.worker-profile=1") {
		t.Error("worker-profile drop-in was not preserved")
	}

	for _, unit := range []string{"hololive-api", "hololive-alarm-worker"} {
		dropIn := unit + ".container.d/40-x-spaces.conf"
		if !sameFile(t, harness.Path(t, "deploy/quadlet/optional/x-spaces/"+dropIn), h.installed(dropIn)) {
			t.Errorf("x-spaces drop-in not installed for %s", unit)
		}
	}

	if harness.Mode(t, h.at("usr-local-libexec/hololive-bot/quadlet-prestart"))&0o111 == 0 {
		t.Error("prestart helper not installed")
	}

	for _, lib := range []string{"admin-bind.sh", "public-bind-mounts.sh", "postgres-capacity.sh", "compose-env.sh", "postgres-endpoint.sh", "collector-slots.sh", "collector-slots.tsv"} {
		if !sameFile(t, filepath.Join(repo, "scripts/deploy/lib", lib), h.at("usr-local-libexec/hololive-bot/lib/"+lib)) {
			t.Errorf("prestart dependency not installed: %s", lib)
		}
	}

	assertInstalledLibraries(t, h)
	assertInstalledTree(t, h, repo)
}

func assertInstalledTree(t *testing.T, h syncHost, repo string) {
	t.Helper()

	if !exists(h.at("etc-systemd-system/admin-dashboard-ingress-firewall.service")) {
		t.Error("firewall unit not installed")
	}

	if exists(h.at("opt/current/marker")) || !exists(h.at("opt/current/deploy/postgres/pg_hba.conf")) {
		t.Error("current tree was not replaced by the staged tree")
	}

	if !h.hasEvent(t, "systemctl daemon-reload") {
		t.Error("daemon-reload did not run after installation")
	}

	if got := harness.Read(t, h.at("opt/current/.source-revision")); got != head(t, repo) && got != head(t, repo)+"\n" {
		t.Errorf("deploy tree records %q, want the archived revision", got)
	}

	if !strings.Contains("\n"+h.events(t), "\nverify ") {
		t.Error("exec-tree ownership verification did not run")
	}

	if left := glob(t, h.at("opt/.quadlet-check.*")); len(left) > 0 {
		t.Error("Quadlet check directory was left behind")
	}
}

func assertInstalledLibraries(t *testing.T, h syncHost) {
	t.Helper()

	lib := h.at("usr-local-libexec/hololive-bot/lib") + "/"
	checks := []struct {
		label, script string
		args          []string
	}{
		{
			"endpoint", `. "$1"; . "$2"; postgres_endpoint_quadlet "$3" "$4" >/dev/null`,
			[]string{lib + "compose-env.sh", lib + "postgres-endpoint.sh", h.installed("hololive-api.container"), h.compose()},
		},
		{
			"capacity", `. "$1"; postgres_capacity_assert_policy_target "$2" "$3" >/dev/null`,
			[]string{lib + "postgres-capacity.sh", h.at("opt/current/scripts/ci/postgres-capacity-policy.tsv"), h.compose()},
		},
		{"validator", `. "$1"; compose_env_validate_file_format "$2"`, []string{lib + "compose-env.sh", h.compose()}},
	}

	for _, c := range checks {
		harness.Succeeds(t, "installed "+c.label+" library", harness.Bash(t, harness.Cmd{Script: c.script, Args: c.args}))
	}

	invalid := filepath.Join(t.TempDir(), "invalid.env")
	put(t, invalid, "KEY=value \n")
	harness.Fails(t, "installed validator accepts surrounding whitespace",
		harness.Bash(t, harness.Cmd{Script: `. "$1"; compose_env_validate_file_format "$2"`, Args: []string{lib + "compose-env.sh", invalid}}))
}

func inodeAndMtime(t *testing.T, paths ...string) []string {
	t.Helper()

	out := make([]string, 0, len(paths))

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("stat %s: no inode", path)
		}

		out = append(out, strings.Join([]string{path, itoa(st.Ino), itoa(uint64(info.ModTime().Unix()))}, ":")) // #nosec G115 -- fixture mtime은 양수다.
	}

	return out
}

func itoa(n uint64) string {
	return strconv.FormatUint(n, 10)
}

func backupInodes(t *testing.T, h syncHost) []string {
	t.Helper()

	return inodeAndMtime(t, backups(t, h, "")...)
}

func assertValkeyOnlyLeavesOthers(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	protected := []string{h.installed("hololive-api.container"), h.installed("holo-postgres.container"), h.at("opt/current/.source-revision")}
	preserved := inodeAndMtime(t, protected...)
	recovery := backupInodes(t, h)

	appendFile(t, h.installed("valkey-cache.container"), "\n")
	e.syncs(t, h, repo, "Valkey-only publication", "--valkey-only")

	if !sameFile(t, h.installed("valkey-cache.container"), filepath.Join(repo, "deploy/quadlet/hololive-bot/valkey-cache.container")) {
		t.Fatal("Valkey candidate was not published")
	}

	if strings.Join(inodeAndMtime(t, protected...), "\n") != strings.Join(preserved, "\n") ||
		strings.Join(backupInodes(t, h), "\n") != strings.Join(recovery, "\n") {
		t.Fatal("Valkey-only publication changed other resources")
	}
}

func assertValkeyBindAndStartChecks(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	appendFile(t, h.installed("valkey-cache.container"), "\n")
	h.setCompose(t, "VALKEY_BIND_IP", "0.0.0.0")
	e.rejects(t, h, repo, "Valkey wildcard bind", "VALKEY_BIND_IP must select the central Valkey endpoint", "--valkey-only")
	h.setCompose(t, "VALKEY_BIND_IP", "100.100.1.8")

	for label, pair := range map[string][2]string{
		"stale Valkey prestart":   {"usr-local-libexec/hololive-bot/quadlet-prestart", "scripts/deploy/quadlet-prestart.sh"},
		"stale Valkey bind check": {"usr-local-libexec/hololive-bot/lib/admin-bind.sh", "scripts/deploy/lib/admin-bind.sh"},
		"stale Valkey topology":   {"opt/current/deploy/topology/hosts.conf", "deploy/topology/hosts.conf"},
	} {
		appendFile(t, h.at(pair[0]), "\n")
		e.rejects(t, h, repo, label, staleStart, "--valkey-only")
		overwrite(t, filepath.Join(repo, pair[1]), h.at(pair[0]))
	}
}

func assertValkeyImages(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	missing := h.at("events.image-missing")
	put(t, missing, defaultValkeyImage+"\n")
	e.rejects(t, h, repo, "missing Valkey image", "Valkey image is not present: "+defaultValkeyImage, "--valkey-only")
	h.setCompose(t, "VALKEY_IMAGE", pinnedValkeyImage)
	put(t, missing, pinnedValkeyImage+"\n")
	e.rejects(t, h, repo, "missing pinned Valkey image", "Valkey image is not present: "+pinnedValkeyImage, "--valkey-only")
	h.setCompose(t, "VALKEY_IMAGE", "-")
	harness.Remove(t, missing)

	appendFile(t, h.installed("valkey-cache.container"), "\n")
	h.setCompose(t, "VALKEY_IMAGE", "")
	e.rejects(t, h, repo, "empty Valkey image", "VALKEY_IMAGE is empty", "--valkey-only")
	h.setCompose(t, "VALKEY_IMAGE", `""`)
	e.rejects(t, h, repo, "empty quoted Valkey image", "VALKEY_IMAGE is empty", "--valkey-only")
	h.setCompose(t, "VALKEY_IMAGE", pinnedValkeyImage)
	put(t, missing, defaultValkeyImage+"\n")
	e.syncs(t, h, repo, "pinned Valkey image", "--valkey-only")

	if !sameFile(t, h.installed("valkey-cache.container"), filepath.Join(repo, "deploy/quadlet/hololive-bot/valkey-cache.container")) {
		t.Fatal("pinned-image Valkey candidate was not published")
	}

	h.setCompose(t, "VALKEY_IMAGE", "-")
	harness.Remove(t, missing)

	for _, path := range []string{
		"/usr/local/libexec/hololive-bot/quadlet-prestart", "/usr/local/libexec/hololive-bot/lib/collector-slots.tsv",
		"/opt/hololive-bot/compose/current/deploy/topology/hosts.conf", "/opt/iris-stack/valkey-ha/stage.py",
	} {
		if !h.hasEvent(t, "verify-path "+path) {
			t.Errorf("Valkey start check ownership was not verified: %s", path)
		}
	}
}

func podmanChown(t *testing.T, owner, path string) {
	t.Helper()
	run(t, "", "podman", "unshare", "/usr/bin/chown", owner, path)
}

func assertValkeyStartCheckOwnership(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	appendFile(t, h.installed("valkey-cache.container"), "\n")

	check := h.at("usr-local-libexec/hololive-bot/lib/collector-slots.tsv")
	appendFile(t, check, "\n")
	e.rejects(t, h, repo, "stale Valkey slot table", staleStart, "--valkey-only")
	overwrite(t, filepath.Join(repo, "scripts/deploy/lib/collector-slots.tsv"), check)
	chmod(t, check, 0o664)
	e.rejects(t, h, repo, "Valkey start check mode", staleStart, "--valkey-only")
	chmod(t, check, 0o644)
	rename(t, check, check+".real")
	harness.Symlink(t, "collector-slots.tsv.real", check)
	e.rejects(t, h, repo, "symlinked Valkey start check", staleStart, "--valkey-only")
	harness.Remove(t, check)
	rename(t, check+".real", check)
	podmanChown(t, "1", check)
	e.rejects(t, h, repo, "non-root Valkey start check", staleStart, "--valkey-only")
	podmanChown(t, "0", check)

	denied := h.at("events.ownership-denied")
	put(t, denied, "/usr/local/libexec/hololive-bot/lib/collector-slots.tsv\n")
	e.rejects(t, h, repo, "unsafe Valkey start check path", "installed Valkey start checks are writable by a non-root user", "--valkey-only")
	put(t, denied, "/opt/iris-stack/valkey-ha/stage.py\n")
	e.rejects(t, h, repo, "unsafe Valkey stage tool path", "Valkey stage tool is missing or writable by a non-root user", "--valkey-only")
	e.rejects(t, h, repo, "unsafe Valkey stage tool path in a full sync", "Valkey stage tool is missing or writable by a non-root user")
	harness.Remove(t, denied)
	e.rejectsOpt(t, h, repo, h.at("opt"), "other Valkey runtime tree", "Valkey-only publication checks the runtime tree that the prestart reads", "--valkey-only")
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func rename(t *testing.T, from, to string) {
	t.Helper()

	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
}

const mcsLevel = "[Container]\nSecurityLabelLevel=s0:c900,c901\n"

func assertValkeyOverrides(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	put(t, h.installed("valkey-cache.container.d/50-mcs-level.conf"), mcsLevel)
	e.rejects(t, h, repo, "local Valkey drop-in", "expected an existing Valkey unit without local drop-ins", "--valkey-only")
	removeAll(t, h.installed("valkey-cache.container.d"))

	put(t, h.at("etc-systemd-system/valkey-cache.service.d/override.conf"), "[Service]\nExecStartPre=\n")
	e.rejects(t, h, repo, "Valkey service drop-in", unsupportedOrigin, "--valkey-only")
	removeAll(t, h.at("etc-systemd-system/valkey-cache.service.d"))

	for _, flag := range []string{"alias-dropin", "alias", "not-found"} {
		put(t, h.at("events."+flag), "")
		e.rejects(t, h, repo, "Valkey "+flag, unsupportedOrigin, "--valkey-only")
		harness.Remove(t, h.at("events."+flag))
	}

	assertValkeyMasks(t, e, h, repo)

	appendFile(t, h.installed("valkey-cache.container"), "\n")

	linked := filepath.Join(e.tmp, "linked-unit/valkey-cache.service")
	put(t, linked, "[Service]\nExecStart=/bin/true\n")
	harness.Symlink(t, linked, h.at("etc-systemd-system/staging-cache.service"))
	e.syncs(t, h, repo, "separate linked unit outside the search path", "--valkey-only")
	harness.Remove(t, h.at("etc-systemd-system/staging-cache.service"))

	appendFile(t, h.installed("valkey-cache.container"), "\n")
	put(t, h.at("etc-containers-systemd/container.d/60-image.conf"), "[Container]\nImage=localhost/other:tag\n")
	e.rejects(t, h, repo, "inherited Quadlet drop-in", unsupportedOrigin, "--valkey-only")
	removeAll(t, h.at("etc-containers-systemd/container.d"))
	put(t, h.at("etc-containers-systemd/valkey-cache.container.d/50-mcs-level.conf"), mcsLevel)
	e.syncs(t, h, repo, "host MCS drop-in", "--valkey-only")
	removeAll(t, h.at("etc-containers-systemd/valkey-cache.container.d"))
}

func assertValkeyMasks(t *testing.T, e syncEnv, h syncHost, repo string) {
	t.Helper()

	unit := h.at("etc-systemd-system/valkey-cache.service")
	alias := h.at("etc-systemd-system/valkey-alias.service")

	harness.Symlink(t, "/dev/null", unit)
	harness.Symlink(t, "/run/systemd/generator/valkey-cache.service", alias)
	e.rejects(t, h, repo, "masked Valkey unit with an alias", unsupportedOrigin, "--valkey-only")
	harness.Remove(t, alias)

	for _, target := range []string{
		"/etc/systemd/system/held/valkey-cache.service", "held/valkey-cache.service",
		"/etc/systemd/system/multi-user.target.wants/valkey-cache.service", "../system/held/../valkey-cache.service",
		"held/valkey-cache.service/", "/etc/systemd/system/held/valkey-cache.service/.", "held/valkey-cache.service//",
	} {
		harness.Symlink(t, target, alias)
		e.rejects(t, h, repo, "masked Valkey unit with an alias to "+target, unsupportedOrigin, "--valkey-only")
		harness.Remove(t, alias)
	}

	runtimeMask := h.at("run/systemd/system/valkey-cache.service")
	mkdirs(t, filepath.Dir(runtimeMask))
	harness.Symlink(t, "/dev/null", runtimeMask)
	e.rejects(t, h, repo, "Valkey runtime mask under the persistent mask", unsupportedOrigin, "--valkey-only")
	harness.Remove(t, runtimeMask)

	harness.Remove(t, unit)
	harness.Symlink(t, "/run/systemd/generator/valkey-cache.service", unit)
	e.rejects(t, h, repo, "Valkey mask not pointing to /dev/null", unsupportedOrigin, "--valkey-only")
	harness.Remove(t, unit)
	harness.Symlink(t, "/dev/null", unit)
	e.syncs(t, h, repo, "persistently masked Valkey unit", "--valkey-only")
	harness.Remove(t, unit)
}

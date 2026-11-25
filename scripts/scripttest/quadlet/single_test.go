package quadlet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestDeployMigratesWithTheCandidateBeforeActivation(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	succeeds(t, "success", c.deploy(t, "success"))
	c.assertOrder(t, "success",
		"pair capture hololive-api previous", "pair rollback hololive-api previous", "pair check hololive-api previous",
		"pair candidate hololive-api candidate", "pair check hololive-api candidate", "pair verify-source hololive-api candidate",
		"systemctl daemon-reload", "systemctl start hololive-db-migrate.service",
		"writer-sql hololive_runtime", "pair activate hololive-api candidate", "systemctl daemon-reload", "systemctl restart hololive-api.service",
		"pair verify-live hololive-api candidate")
	c.assertNoCall(t, "success", `^pair (prepare|activate hololive-api previous)`)

	if c.installedPair(t, api) != pairCandidate || read(t, c.state("runtime")) != pairCandidate {
		t.Fatal("candidate pair is not active")
	}

	if read(t, c.state("migrated-with")) != "sha256:"+apiHex {
		t.Fatal("migration did not use the candidate image")
	}

	release := c.release(t)
	if read(t, filepath.Join(release, "nrestarts-before")) != "3" {
		t.Fatal("NRestarts baseline was not recorded")
	}

	if hasQuadlet() {
		units := filepath.Join(t.TempDir(), "units")
		copyDir(t, harness.Path(t, "deploy/quadlet/hololive-bot"), units)
		copyDir(t, c.quadlet("hololive-db-migrate.container.d"), units+"/")

		execStart, ok := quadletExecStart(t, units, "hololive-db-migrate")
		if !ok || !strings.HasSuffix(execStart, " sha256:"+apiHex+" --schema-only") {
			t.Fatalf("card migrator image or mode differs: %q", execStart)
		}
	}

	c.clearCalls(t)
	succeeds(t, "rollback check", c.rollbackRelease(t, "rollback_check", release, "--check"))
	c.assertOrder(t, "rollback check", "pair verify hololive-api previous", "pair check hololive-api previous")
	c.assertNoCall(t, "rollback check", `^(pair activate |systemctl )`)
}

func TestDeployOutcomeLines(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	c.assertOutput(t, c.deploy(t, "success"), `^\[OUTCOME\] deployed service=hololive-api`)
}

func TestFirstAdoptionPinsTheRunningImage(t *testing.T) {
	t.Parallel()

	c := newCase(t, worker, false, revision)
	succeeds(t, "first adoption", c.run(t, "first_adoption", workerHex, helper(t), worker, workerImage, apiCandidate))
	c.assertOrder(t, "first adoption",
		"pair prepare hololive-alarm-worker previous", "systemctl daemon-reload", "pair candidate hololive-alarm-worker candidate",
		"systemctl start hololive-db-migrate.service", "pair activate hololive-alarm-worker candidate",
		"systemctl restart hololive-alarm-worker.service", "pair verify-live hololive-alarm-worker candidate")
	c.assertNoCall(t, "first adoption", `^pair capture `)

	if read(t, c.state("migrated-with")) != "sha256:"+apiHex || c.installedPair(t, worker) != pairCandidate {
		t.Fatal("first adoption did not migrate with the API image and activate the candidate")
	}
}

func TestMigrationFailureStopsBeforeActivation(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"migration_failure", "fence_during_migration"} {
		c := newCase(t, api, true, revision)
		r := c.deploy(t, name)
		fails(t, name, r)
		c.assertNoCall(t, name, `^(pair activate |systemctl restart )`)

		if c.installedPair(t, api) != pairBaseline || read(t, c.state("runtime")) != pairBaseline {
			t.Fatalf("%s: running pair changed", name)
		}

		c.assertOutput(t, r, `^\[OUTCOME\] not_activated service=hololive-api stage=migration`)
	}
}

func TestFailureAfterActivationNeedsAnApprovedRollback(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"health_failure", "auto_restart"} {
		c := newCase(t, api, true, revision)
		r := c.deploy(t, name)
		fails(t, name, r)
		c.assertOrder(t, name, "pair activate hololive-api candidate", "systemctl restart hololive-api.service")
		c.assertNoCall(t, name, `^pair activate hololive-api previous`)

		if c.installedPair(t, api) != pairCandidate {
			t.Fatalf("%s: helper replaced the candidate pair by itself", name)
		}

		c.assertOutput(t, r, `^\[OUTCOME\] failed_after_activation service=hololive-api .*compatibility is approved`)
		c.clearCalls(t)
		succeeds(t, name+" rollback", c.rollbackRelease(t, name, c.release(t), "--apply"))
		c.assertOrder(t, name+" rollback",
			"pair verify hololive-api previous", "pair check hololive-api previous", "pair activate hololive-api previous",
			"systemctl daemon-reload", "systemctl restart hololive-api.service", "pair verify-live hololive-api previous")

		if c.installedPair(t, api) != "previous" || read(t, c.state("runtime")) != "previous" {
			t.Fatalf("%s: approved rollback did not restore the previous pair", name)
		}
	}
}

func TestRollbackRestoresThePreviousBaseUnit(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	unit := c.quadlet("hololive-api.container")
	put(t, c.state("new-unit"), harness.Read(t, unit))
	put(t, c.state("previous-unit"), "# previous revision definition\n"+harness.Read(t, unit))
	fails(t, "unit changed", c.deploy(t, "health_failure"))

	if !sameFile(t, unit, c.state("new-unit")) {
		t.Fatal("helper changed the base unit without an approved rollback")
	}

	succeeds(t, "approved rollback", c.rollbackRelease(t, "health_failure", c.release(t), "--apply"))

	if !sameFile(t, unit, c.state("previous-unit")) {
		t.Fatal("rollback kept the new base unit definition with the previous image")
	}

	if !sameFile(t, filepath.Join(c.release(t), "rollback-replaced-hololive-api.container"), c.state("new-unit")) {
		t.Fatal("the replaced base unit was not retained in the release")
	}
}

func (c *deployCase) priorRelease(t *testing.T, unit string) string {
	t.Helper()

	release := c.current("artifacts/worker-profile/releases/prior")
	put(t, filepath.Join(release, "service"), "hololive-api\n")
	put(t, filepath.Join(release, "previous/profile.json"), "profile\n")
	put(t, filepath.Join(release, "previous/pair.json"), `{"image":"sha256:`+zeros(8)+`"}`+"\n")
	put(t, filepath.Join(release, "previous/50-worker-profile.conf"), "pair=previous\napp_version=captured\n")

	if unit != "" {
		put(t, filepath.Join(release, "previous/unit.container"), unit)
	}

	return release
}

func TestRollbackComparesEffectiveEndpoints(t *testing.T) {
	t.Parallel()

	extra := map[string]string{
		"changed":    "",
		"override":   "HOLOLIVE_CENTRAL_POSTGRES_HOST=holo-postgres\nHOLOLIVE_CENTRAL_POSTGRES_PORT=5432\n",
		"missing":    "HOLOLIVE_CENTRAL_POSTGRES_HOST=\n",
		"invalid":    "HOLOLIVE_CENTRAL_POSTGRES_PORT=65536\n",
		"unresolved": "HOLOLIVE_CENTRAL_POSTGRES_HOST=${UNKNOWN_ENDPOINT}\n",
	}

	for name, env := range extra {
		c := newCase(t, api, true, revision)

		var previous strings.Builder

		for line := range strings.Lines(harness.Read(t, c.quadlet("hololive-api.container"))) {
			switch {
			case strings.HasPrefix(line, "Environment=HOLOLIVE_CENTRAL_POSTGRES_HOST="):
				line = "Environment=HOLOLIVE_CENTRAL_POSTGRES_HOST=other-pooler\n"
			case strings.HasPrefix(line, "Environment=HOLOLIVE_CENTRAL_POSTGRES_PORT="):
				line = "Environment=HOLOLIVE_CENTRAL_POSTGRES_PORT=6000\n"
			}

			previous.WriteString(line)
		}

		put(t, c.state("previous-unit"), previous.String())
		c.priorRelease(t, previous.String())
		appendText(t, c.compose(), env)

		for _, mode := range []string{"--check", "--apply"} {
			c.clearCalls(t)

			r := c.rollbackRelease(t, "health_failure", c.release(t), mode)

			if name == "override" {
				succeeds(t, "same effective endpoint rollback "+mode, r)

				continue
			}

			fails(t, "unsafe endpoint rollback "+name+" "+mode, r)
			c.assertNoCall(t, "endpoint rollback rejected", `^(pair activate |systemctl )`)

			if !exists(filepath.Join(c.release(t), "previous/unit.container")) {
				t.Fatal("rollback recovery unit lost")
			}
		}
	}
}

func TestDeployAndRollbackRefuseAHeldWriterLock(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	release := c.holdLock(t)

	defer release()

	r := c.deploy(t, "success")
	fails(t, "concurrent deploy", r)
	c.assertOutput(t, r, "another central Quadlet writer is running")

	if harness.Size(t, c.state("calls")) != 0 {
		t.Fatal("helper made calls while locked out")
	}

	r = c.rollbackRelease(t, "success", c.current("artifacts/worker-profile/releases/missing"), "--apply")
	fails(t, "concurrent rollback", r)
	c.assertOutput(t, r, "another central Quadlet writer is running")

	if harness.Size(t, c.state("calls")) != 0 {
		t.Fatal("rollback made calls while locked out")
	}
}

func TestFailedRestorationRetainsTheReleasePairs(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	fails(t, "restore failure deploy", c.deploy(t, "restore_failure"))

	release := c.release(t)
	fails(t, "restore failure rollback", c.rollbackRelease(t, "restore_failure", release, "--apply"))

	if !exists(filepath.Join(release, "previous/profile.json")) || !exists(filepath.Join(release, "candidate/profile.json")) {
		t.Fatal("release pairs were not retained")
	}
}

func TestPreflightRejectsBeforeAnyChange(t *testing.T) {
	t.Parallel()

	single := newCase(t, api, true, revision)
	fails(t, "single writer before card", single.deploy(t, "single_before_card"))
	single.assertNoCall(t, "single_before_card", `^(pair |systemctl (stop|start|restart|daemon-reload))`)

	mismatch := newCase(t, worker, true, otherRevision)
	mismatchResult := mismatch.run(t, "revision_mismatch", workerHex, helper(t), worker, workerImage, apiCandidate)
	fails(t, "revision mismatch", mismatchResult)

	capacity := newCase(t, api, true, revision)
	appendText(t, capacity.compose(), "BOT_POSTGRES_POOL_MAX_CONNS=40\n")

	capacityResult := capacity.deploy(t, "capacity")
	fails(t, "capacity", capacityResult)

	for c, r := range map[*deployCase]harness.Result{mismatch: mismatchResult, capacity: capacityResult} {
		c.assertNoCall(t, "preflight", `^(pair |systemctl (daemon-reload|start|restart))`)

		if exists(c.current("artifacts")) {
			t.Fatal("preflight created release storage")
		}

		c.assertOutput(t, r, `^\[OUTCOME\] not_activated .* stage=preflight`)
	}
}

func (c *deployCase) assertUntouched(t *testing.T, label string) {
	t.Helper()

	if exists(c.state("migrated-with")) || exists(c.current("artifacts")) {
		t.Fatalf("%s: rejection changed deployment state", label)
	}
}

func TestDeployRejectsUnsupportedOverrideOrigins(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"etc/containers/systemd/hololive-bot/container.d/60-pool.conf",
		"etc/containers/systemd/hololive-bot/hololive-.container.d/60-pool.conf",
		"etc/containers/systemd/hololive-bot/hololive-collector-.container.d/60-pool.conf",
		"etc/containers/systemd/container.d/60-pool.conf",
		"run/containers/systemd/hololive-api.container.d/60-pool.conf",
		"run/containers/systemd/users/hololive-.container.d/60-pool.conf",
		"usr/share/containers/systemd/nested/hololive-.container.d/60-pool.conf",
		"run/containers/systemd/nested/hololive-api.container",
		"etc/systemd/system/hololive-api.service.d/60-pool.conf",
		"run/systemd/system/hololive-.service.d/60-pool.conf",
		"usr/lib/systemd/system/service.d/60-pool.conf",
		"run/systemd/generator/hololive-api.service.d/60-pool.conf",
		"run/systemd/generator.early/hololive-api.service",
		"custom-systemd/hololive-api.service.d/60-pool.conf",
		"etc/systemd/system/hololive-api.service",
	} {
		c := newCase(t, api, true, revision)
		section := "Container"

		if strings.Contains(origin, "/systemd/system/") || strings.Contains(origin, "/systemd/generator") || strings.HasPrefix(origin, "custom-systemd/") {
			section = "Service"
		}

		put(t, c.at(origin), "["+section+"]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")

		r := c.deploy(t, "success")
		fails(t, origin, r)
		c.assertOutput(t, r, originDenied)
		c.assertNoCall(t, origin, `^(pair |systemctl (daemon-reload|start|restart|stop|mask|unmask))`)

		if c.installedPair(t, api) != pairBaseline {
			t.Fatalf("%s: origin rejection changed the installed pair", origin)
		}

		c.assertUntouched(t, origin)
	}

	for _, state := range []string{"manager-environment", "manager-reload-required"} {
		c := newCase(t, api, true, revision)
		put(t, c.state(state), "QUADLET_UNIT_DIRS=/unreviewed\n")

		r := c.deploy(t, "success")
		fails(t, state, r)
		c.assertOutput(t, r, originDenied)
		c.assertNoCall(t, state, `^(pair |systemctl (daemon-reload|start|restart|stop|mask|unmask))`)
		c.assertUntouched(t, state)
	}
}

func TestDeployRejectsExecutionOverrides(t *testing.T) {
	t.Parallel()

	for _, directive := range []string{"PodmanArgs=--env ADMIN_API_POSTGRES_POOL_MAX_CONNS=5", "GlobalArgs=--module=/unreviewed.conf", "EnvironmentHost=true"} {
		c := newCase(t, api, true, revision)
		put(t, c.quadlet("hololive-api.container.d/60-pool.conf"), "[Container]\n"+directive+"\n")

		r := c.deploy(t, "success")
		fails(t, directive, r)
		c.assertOutput(t, r, "unsupported Quadlet execution override:")
		c.assertNoCall(t, directive, `^(pair |systemctl (daemon-reload|start|restart|stop))`)
	}
}

func TestDeployKeepsNeutralMCSPolicies(t *testing.T) {
	t.Parallel()

	for _, level := range []string{"s0:c900,c901", "s0:c902,c903"} {
		c := newCase(t, api, true, revision)
		policy := c.at("etc/containers/systemd/hololive-api.container.d/70-security.conf")
		put(t, policy, "[Container]\nSecurityLabelLevel="+level+"\n")
		succeeds(t, "neutral MCS "+level, c.deploy(t, "success"))

		if harness.Read(t, policy) != "[Container]\nSecurityLabelLevel="+level+"\n" || c.installedPair(t, api) != pairCandidate {
			t.Fatalf("%s: deployment rewrote the MCS policy or did not activate", level)
		}
	}

	c := newCase(t, api, true, revision)
	put(t, c.at("etc/containers/systemd/hololive-api.container.d/70-security.conf"),
		"[Container]\nSecurityLabelLevel=s0:c900,c901\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")

	r := c.deploy(t, "success")
	fails(t, "mixed security and capacity", r)
	c.assertOutput(t, r, originDenied)
	c.assertNoCall(t, "mixed", `^(pair |systemctl (daemon-reload|start|restart|stop))`)
	c.assertUntouched(t, "mixed security and capacity")
}

func TestQuadletAppliesInheritedOverrides(t *testing.T) {
	t.Parallel()

	if !hasQuadlet() {
		t.Skip("podman quadlet generator is not installed")
	}

	units := filepath.Join(t.TempDir(), "units")
	copyDir(t, harness.Path(t, "deploy/quadlet/hololive-bot"), units)
	put(t, filepath.Join(units, "hololive-.container.d/60-pool.conf"), "[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")

	if execStart, ok := quadletExecStart(t, units, api); !ok || !strings.Contains(execStart, "ADMIN_API_POSTGRES_POOL_MAX_CONNS=5") {
		t.Fatalf("actual Quadlet did not apply the inherited capacity override: %q", execStart)
	}

	put(t, filepath.Join(units, "hololive-.container.d/60-pool.conf"), "[Container]\nSecurityLabelLevel=s0:c900,c901\n")

	if execStart, ok := quadletExecStart(t, units, api); !ok || !strings.Contains(execStart, "label=level:s0:c900,c901") ||
		strings.Contains(execStart, "ADMIN_API_POSTGRES_POOL_MAX_CONNS=5") {
		t.Fatalf("actual Quadlet did not retain only the neutral MCS setting: %q", execStart)
	}
}

func TestRollbackValidatesRetainedUnitCaps(t *testing.T) {
	t.Parallel()

	live := newCase(t, api, true, revision)
	appendText(t, live.compose(), "ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
	succeeds(t, "increased capacity within the budget", live.deploy(t, "success"))

	if live.installedPair(t, api) != pairCandidate {
		t.Fatal("matching capacity did not activate")
	}

	for _, name := range []string{"unit_default", "compose_override", "dropin_override", "exhausted"} {
		c := newCase(t, api, true, revision)
		unit := c.quadlet("hololive-api.container")
		put(t, c.state("previous-unit"), harness.Read(t, unit)+"\n[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
		succeeds(t, name+" fixture deployment", c.deploy(t, "success"))

		release := c.release(t)

		switch name {
		case "compose_override":
			appendText(t, c.compose(), "ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n")
		case "dropin_override":
			put(t, unit+".d/60-pool.conf", "[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=4\n")
		case "exhausted":
			appendText(t, c.compose(), "BOT_POSTGRES_POOL_MAX_CONNS=40\n")
		}

		for _, mode := range []string{"--check", "--apply"} {
			c.clearCalls(t)

			r := c.rollbackRelease(t, "success", release, mode)

			if name != "exhausted" {
				succeeds(t, name+" rollback "+mode, r)

				continue
			}

			fails(t, "exhausted rollback "+mode, r)
			c.assertNoCall(t, "capacity", `^(pair activate |systemctl )`)

			if sameFile(t, unit, filepath.Join(release, "previous/unit.container")) {
				t.Fatal("unsafe rollback replaced the base unit")
			}

			if c.installedPair(t, api) != pairCandidate || !exists(filepath.Join(release, "previous/profile.json")) {
				t.Fatal("capacity rejection lost the current pair or its recovery")
			}
		}
	}
}

func TestLegacyReleaseRestoresOnlyWithItsOverlay(t *testing.T) {
	t.Parallel()

	c := newCase(t, api, true, revision)
	release := c.priorRelease(t, "")

	for _, mode := range []string{"--check", "--apply"} {
		succeeds(t, "legacy release "+mode, c.rollbackRelease(t, "success", release, mode))
	}

	image := c.state("images/sha256_" + zeros(8))

	for _, contract := range []string{"absent", "incompatible"} {
		var kept strings.Builder

		for line := range strings.Lines(harness.Read(t, image)) {
			if !strings.HasPrefix(line, "schema=") {
				kept.WriteString(line)
			}
		}

		if contract == "incompatible" {
			kept.WriteString("schema=other\n")
		}

		put(t, image, kept.String())

		for _, mode := range []string{"--check", "--apply"} {
			c.clearCalls(t)
			fails(t, contract+" outbox schema "+mode, c.rollbackRelease(t, "success", release, mode))
			c.assertNoCall(t, contract, `^(pair activate |systemctl (restart|start|daemon-reload))`)
		}
	}

	if exists(filepath.Join(release, "rollback.conf")) || exists(filepath.Join(release, "previous.endpoint")) {
		t.Fatal("legacy release gained retroactive transition evidence")
	}

	if _, err := os.Stat(release); err != nil {
		t.Fatal(err)
	}
}

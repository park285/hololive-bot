package quadlet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const heldResumeCalls = `^(writer-sql |pair activate |systemctl (daemon-reload|unmask|start|restart))`

func TestCoordinatedResumeStartsOnlyTheCompleteCandidatePair(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)
	c.assertOrder(t, "quiesce",
		"guard arm hololive-alarm-worker.service", "guard arm hololive-api.service",
		"guard check hololive-alarm-worker.service", "guard check hololive-api.service",
		"pair verify-captured hololive-alarm-worker previous", "guard check hololive-alarm-worker.service",
		"systemctl stop hololive-alarm-worker.service", "journal stop hololive-alarm-worker.service",
		"exit-status hololive-alarm-worker.service",
		"pair verify-captured hololive-api previous", "guard check hololive-api.service",
		"systemctl stop hololive-api.service", "journal stop hololive-api.service",
		"exit-status hololive-api.service",
		"guard seal hololive-alarm-worker.service", "guard seal hololive-api.service")

	c.clearCalls(t)
	appendText(t, c.compose(), "BOT_POSTGRES_POOL_MAX_CONNS=40\n")
	fails(t, "exhausted held resume", c.resume(t, "success", apiRelease, workerRelease))
	c.assertHeldPair(t)
	c.assertNoCall(t, "capacity", `^(pair activate |systemctl (unmask|start|restart|daemon-reload))`)

	if !exists(filepath.Join(apiRelease, "previous/profile.json")) || !exists(filepath.Join(workerRelease, "previous/profile.json")) {
		t.Fatal("capacity rejection discarded retained recovery pairs")
	}

	put(t, c.compose(), "VALKEY_BIND_IP=100.100.1.8\n")

	c.serverEpoch = epochNew
	succeeds(t, "coordinated resume", c.resume(t, "success", c.coordinatedRelease(t, api), c.coordinatedRelease(t, worker)))
	c.assertCandidatePairRunning(t)
	c.assertOrder(t, "resume",
		"writers-gate", "systemctl start hololive-db-migrate.service", "writer-sql hololive_alarm_worker", "writer-sql hololive_runtime",
		"pair activate hololive-alarm-worker candidate", "pair activate hololive-api candidate",
		"systemctl unmask hololive-alarm-worker.service", "systemctl start hololive-alarm-worker.service",
		"journal start hololive-alarm-worker.service 71", "pair verify-live hololive-alarm-worker candidate",
		"systemctl unmask hololive-api.service", "systemctl start hololive-api.service",
		"journal start hololive-api.service 72", "pair verify-live hololive-api candidate")
	succeeds(t, "completed opening allows a single deploy", c.deploy(t, "success"))
}

func TestQuiesceRefusesUnsafeOriginals(t *testing.T) {
	t.Parallel()

	pooled := coordinatedCase(t)
	put(t, pooled.state("running-endpoint"), "POSTGRES_HOST=other-pooler\nPOSTGRES_PORT=6000\n")
	appendText(t, pooled.compose(), "HOLOLIVE_CENTRAL_POSTGRES_HOST=other-pooler\nHOLOLIVE_CENTRAL_POSTGRES_PORT=6000\n")
	pooled.clearCalls(t)
	fails(t, "pooled original", pooled.quiesceWith(t, "success"))
	pooled.assertNoCall(t, "pooled-candidate", `^(guard arm |systemctl stop )`)

	capped := coordinatedCase(t)
	put(t, capped.quadlet("hololive-api.container.d/60-pool.conf"), "[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=40\n")
	capped.clearCalls(t)
	fails(t, "installed pool cap", capped.quiesceWith(t, "success"))
	capped.assertNoCall(t, "installed-cap", `^(guard arm |systemctl stop )`)
}

func TestHeldPairSurvivesRebootUntilResume(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)

	c.serverEpoch = epochNew
	succeeds(t, "simulate reboot", c.run(t, "success", apiHex, filepath.Join(c.fakebin, "systemctl"), "simulate-reboot"))
	c.assertHeldPair(t)

	if exists(c.state("unsafe-start")) {
		t.Fatal("reboot started a retained old writer against the new Iris")
	}

	succeeds(t, "resume after reboot", c.resume(t, "success", apiRelease, workerRelease))
	c.assertCandidatePairRunning(t)
}

func TestPartialPublicationStaysHeldUntilVerifiedResume(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)

	c.serverEpoch = epochNew
	fails(t, "partial activation", c.resume(t, "api_activation_failure", apiRelease, workerRelease))
	c.assertHeldPair(t)

	if c.installedPair(t, worker) != pairCandidate || c.installedPair(t, api) != pairBaseline {
		t.Fatal("partial activation did not preserve the actual installed pair state")
	}

	succeeds(t, "explicit resume", c.resume(t, "success", apiRelease, workerRelease))
	c.assertCandidatePairRunning(t)

	if c.countCalls(t, "systemctl start hololive-db-migrate.service") != 1 {
		t.Fatal("resuming a partial publication repeated the migration")
	}
}

func TestStaleSourceOrIncompleteHoldCannotResume(t *testing.T) {
	t.Parallel()

	stale := coordinatedCase(t)
	apiRelease, workerRelease := stale.quiesce(t)
	put(t, stale.current(".source-revision"), otherRevision+"\n")

	stale.serverEpoch = epochNew
	fails(t, "stale prepared source", stale.resume(t, "success", apiRelease, workerRelease))
	stale.assertHeldPair(t)

	if exists(stale.state("migrated-with")) {
		t.Fatal("stale preparation reached migration")
	}

	lost := coordinatedCase(t)

	apiRelease, workerRelease = lost.quiesce(t)
	put(t, lost.state("hololive-api/active"), "true\n")

	lost.serverEpoch = epochNew
	fails(t, "running peer", lost.resume(t, "success", apiRelease, workerRelease))

	if read(t, lost.state("hololive-api/active")) != yes || read(t, lost.state("hololive-alarm-worker/active")) != no ||
		read(t, lost.state("hololive-alarm-worker/loadstate")) != "masked" || exists(lost.state("migrated-with")) {
		t.Fatal("incomplete hold triggered migration or changed the other peer")
	}
}

func TestSingleWriterWorkCannotBypassAPartialGuard(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	fails(t, "partial guard", c.quiesceWith(t, "partial_guard"))

	if !exists(c.state("hololive-alarm-worker/guard")) || exists(c.state("hololive-api/guard")) || read(t, c.state("hololive-api/active")) != yes {
		t.Fatal("only the peer should hold a start guard")
	}

	apiRelease := c.coordinatedRelease(t, api)
	c.clearCalls(t)
	fails(t, "single deploy", c.deploy(t, "success"))
	fails(t, "single rollback", c.rollbackRelease(t, "success", apiRelease, "--apply"))
	c.assertNoCall(t, "partial_guard", `^(pair |writer-sql |podman |systemctl )`)
}

func TestIncompleteStopsNeverIssueAReceipt(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{
		"generation_changed", "invocation_changed", "other_stopped_invocation", "residual_pid", "residual_container", "inventory_failure",
		"failed_stop", "stop_command_failed", "abnormal_success_result", "lost_guard", "missing_job", "other_done_job", "journal_failure",
		"gc_exit_143", "missing_exit", "peer_changed",
	} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			c := coordinatedCase(t)
			fails(t, reason, c.quiesceWith(t, reason))

			if exists(filepath.Join(c.coordinatedRelease(t, api), "quiesced")) {
				t.Fatalf("%s: a stop receipt was issued", reason)
			}

			assertIncompleteStop(t, c, reason)
			c.assertNoCall(t, reason, `^(pair activate |systemctl (start|restart|unmask))`)
			c.clearCalls(t)
			fails(t, reason+" resume", c.resume(t, "success", c.coordinatedRelease(t, api), c.coordinatedRelease(t, worker)))
			c.assertNoCall(t, reason, `^(writer-sql |podman exec |pair activate |systemctl (start|stop|restart|unmask))`)
		})
	}
}

func assertIncompleteStop(t *testing.T, c *deployCase, reason string) {
	t.Helper()

	switch reason {
	case "lost_guard", "invocation_changed":
		c.assertNoCall(t, reason, `^systemctl stop `)
	case "peer_changed":
		c.assertNoCall(t, reason, `^systemctl stop hololive-api.service$`)
	case "gc_exit_143":
		stop := filepath.Join(c.coordinatedRelease(t, worker), "stop")
		if !hasLine(t, filepath.Join(stop, "terminal"), "ExecMainCode=0") || !hasLine(t, filepath.Join(stop, "container-exit"), "143") {
			t.Fatal("a collected manager default hid the original container exit 143")
		}
	case "failed_stop", "abnormal_success_result":
		if !hasLine(t, filepath.Join(c.coordinatedRelease(t, worker), "stop/terminal"), "ExecMainStatus=143") {
			t.Fatal("abnormal original exit evidence was lost")
		}

		if !exists(c.state("hololive-alarm-worker/guard")) || !exists(c.state("hololive-api/guard")) {
			t.Fatal("an abnormal stop released the start guards")
		}
	}
}

func hasLine(t *testing.T, path, line string) bool {
	t.Helper()

	data, err := os.ReadFile(path) // #nosec G304 -- fixture 경로만 읽는다.
	if err != nil {
		return false
	}

	for got := range strings.Lines(string(data)) {
		if strings.TrimSuffix(got, "\n") == line {
			return true
		}
	}

	return false
}

func TestUnsafeResumesKeepThePairHeld(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{"foreign_mask", "wrong_owner", "replaced_owner", "mixed_release", "swapped_generation", "legacy_receipt", "other_pooler", "tls"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			c := coordinatedCase(t)
			apiRelease, workerRelease := c.quiesce(t)

			workerRelease = tamperResume(t, c, reason, apiRelease, workerRelease)
			c.clearCalls(t)
			fails(t, reason, c.resume(t, "success", apiRelease, workerRelease))
			c.assertHeldPair(t)
			c.assertNoCall(t, reason, heldResumeCalls)
		})
	}
}

func tamperResume(t *testing.T, c *deployCase, reason, apiRelease, workerRelease string) string {
	t.Helper()

	marker := filepath.Join(filepath.Dir(apiRelease), ".central-writer-hold")

	switch reason {
	case "wrong_owner":
		harness.Remove(t, marker)
		harness.Symlink(t, filepath.Base(workerRelease), marker)
	case "replaced_owner":
		harness.Remove(t, marker)
		put(t, marker, harness.Read(t, filepath.Join(apiRelease, "captured.invocation")))
	case "foreign_mask":
		mask := c.at("etc/systemd/system/hololive-api.service")
		harness.Remove(t, mask)
		harness.Symlink(t, "/dev/null", mask)
	case "mixed_release":
		copied := workerRelease + "-other"
		command(t, "cp", "-a", workerRelease, copied)
		command(t, "ln", "-PT", "--", c.at("etc/systemd/system/hololive-alarm-worker.service"), c.at("etc/systemd/system/."+filepath.Base(copied)+".mask"))

		return copied
	case "swapped_generation":
		put(t, filepath.Join(workerRelease, "previous/pair.json"), `{"container_id":"`+zeros(2)+`"}`+"\n")
	case "legacy_receipt":
		lines := strings.SplitAfter(harness.Read(t, filepath.Join(apiRelease, "quiesced")), "\n")

		lines[0] = "false\nguarded-stop-v1\n"
		put(t, filepath.Join(apiRelease, "quiesced"), strings.Join(lines, ""))
	case "other_pooler":
		appendText(t, c.compose(), "HOLOLIVE_CENTRAL_POSTGRES_HOST=other-pooler\nHOLOLIVE_CENTRAL_POSTGRES_PORT=6000\n")
	case "tls":
		appendText(t, c.compose(), "POSTGRES_SSLMODE=require\n")
	}

	return workerRelease
}

func TestFailedGateOrMigrationKeepsThePairHeld(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{"gate_failure", "sql_failure_hololive_runtime", "sql_failure_hololive_alarm_worker", "migration_failure"} {
		c := coordinatedCase(t)
		apiRelease, workerRelease := c.quiesce(t)
		fails(t, reason, c.resume(t, reason, apiRelease, workerRelease))
		c.assertNoCall(t, reason, `^(pair activate |systemctl (unmask|restart))`)

		if reason == "gate_failure" {
			c.assertNoCall(t, reason, `^systemctl start hololive-db-migrate`)
		}

		c.assertHeldPair(t)
		c.clearCalls(t)
		fails(t, reason+" retry", c.resume(t, reason, apiRelease, workerRelease))
		c.assertNoCall(t, reason, `^(pair activate |systemctl (start|restart|unmask))`)
	}

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)
	fails(t, "PostgreSQL restart during the final API start", c.resume(t, "last_pg_restart", apiRelease, workerRelease))
	c.assertHeldPair(t)

	if !exists(filepath.Join(filepath.Dir(apiRelease), ".central-writer-hold")) {
		t.Fatal("the hold was released before the final infrastructure check")
	}
}

func TestRejectedCandidateStartsStayHeld(t *testing.T) {
	t.Parallel()

	for _, writer := range []string{api, worker} {
		c := coordinatedCase(t)
		apiRelease, workerRelease := c.quiesce(t)
		c.clearCalls(t)

		r := c.resume(t, "verify_failure_"+writer, apiRelease, workerRelease)
		fails(t, writer, r)

		if writer == worker {
			c.assertNoCall(t, writer, `^systemctl (unmask|start|restart|stop) hololive-api.service$`)
			c.assertNoCall(t, writer, `^guard (arm|check|seal) hololive-api.service$`)
		}

		c.assertHeldPair(t)

		if exists(c.state("unguarded-stop")) {
			t.Fatalf("%s: recovery stopped a writer without a start guard", writer)
		}

		if strings.Contains("\n"+r.Stdout+r.Stderr, "\n[OUTCOME] deployed") {
			t.Fatalf("%s: a failed pair was recorded as deployed", writer)
		}

		succeeds(t, "simulate reboot", c.run(t, "success", apiHex, filepath.Join(c.fakebin, "systemctl"), "simulate-reboot"))
		c.assertHeldPair(t)
	}
}

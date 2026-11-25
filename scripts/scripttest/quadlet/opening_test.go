package quadlet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func TestOpeningRejectsIncompleteStates(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{"lost_hold_during_migration", "term_after_unmask", "worker_cid_changed"} {
		c := coordinatedCase(t)
		apiRelease, workerRelease := c.quiesce(t)
		c.clearCalls(t)
		fails(t, reason, c.resume(t, reason, apiRelease, workerRelease))
		c.assertNoCall(t, reason, `^systemctl (start|restart|unmask) hololive-api.service$`)

		switch reason {
		case "lost_hold_during_migration":
			c.assertNoCall(t, reason, `^(writer-sql |pair activate |systemctl unmask )`)
		case "worker_cid_changed":
			if !isDir(filepath.Join(apiRelease, "opening")) || !exists(c.state("hololive-alarm-worker/guard")) || read(t, c.state("hololive-alarm-worker/active")) != yes {
				t.Fatal("a different generation was adopted or the start guard is missing")
			}

			c.assertNoCall(t, reason, `^systemctl stop `)
		default:
			if !isDir(filepath.Join(apiRelease, "opening")) || !exists(c.state("hololive-alarm-worker/guard")) {
				t.Fatal("a signal right after unmask left no durable start guard")
			}

			c.assertNoCall(t, reason, `^systemctl (stop|start|restart) hololive-(alarm-worker|api)\.service$`)
		}
	}
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

func TestOpeningFailuresKeepTheOriginalReceipt(t *testing.T) {
	t.Parallel()

	for _, reason := range []string{
		"term_before_first_unmask", "term_after_first_restart", "replaced_at_launch", interruptedClient,
		"client_failed_done", "client_failed_pending", "client_failed_no_anchor", "client_failed_foreign_cid", "wrong_start_record",
		"rehold_evidence_unwritable", "unmask_reload_failed", "unmask_with_residual_cid",
	} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			c := coordinatedCase(t)
			apiRelease, workerRelease := c.quiesce(t)
			receipt := harness.Read(t, filepath.Join(apiRelease, "quiesced"))
			c.clearCalls(t)

			r := c.resume(t, reason, apiRelease, workerRelease)

			if r.ExitCode == 0 {
				t.Fatalf("%s: a failed resume was accepted", reason)
			}

			c.assertNoCall(t, reason, `^systemctl (start|restart|stop) hololive-api.service$`)
			c.assertNoCall(t, reason, `^systemctl restart `)

			if c.countCalls(t, "systemctl start hololive-db-migrate.service") != 1 {
				t.Fatalf("%s: migration count differs", reason)
			}

			if reason != "term_before_first_unmask" && c.countCalls(t, "systemctl start hololive-alarm-worker.service") != 1 {
				t.Fatalf("%s: the original worker start request was not issued exactly once", reason)
			}

			openingChecks[reason](t, c, openingState{
				reason: reason, status: r.ExitCode, apiRelease: apiRelease, workerRelease: workerRelease,
				opening:       isDir(filepath.Join(apiRelease, "opening")),
				receiptKept:   harness.Read(t, filepath.Join(apiRelease, "quiesced")) == receipt,
				workerGuarded: exists(c.state("hololive-alarm-worker/guard")),
				workerActive:  read(t, c.state("hololive-alarm-worker/active")),
			})
		})
	}
}

type openingState struct {
	reason, workerActive, apiRelease, workerRelease string
	status                                          int
	opening, receiptKept, workerGuarded             bool
}

var openingChecks = map[string]func(t *testing.T, c *deployCase, s openingState){
	"term_before_first_unmask":   assertCancelBeforeUnmask,
	"term_after_first_restart":   assertCancelAfterRestart,
	"client_failed_done":         assertFailedClientReheld,
	"client_failed_pending":      assertFailedClientReheld,
	"replaced_at_launch":         assertUnverifiedGenerationKept,
	interruptedClient:            assertUnverifiedGenerationKept,
	"client_failed_no_anchor":    assertUnverifiedGenerationKept,
	"client_failed_foreign_cid":  assertUnverifiedGenerationKept,
	"wrong_start_record":         assertUnverifiedGenerationKept,
	"rehold_evidence_unwritable": assertReholdWithoutEvidence,
	"unmask_reload_failed":       assertUnmaskUndone,
	"unmask_with_residual_cid":   assertResidualCIDHeld,
}

func assertCancelBeforeUnmask(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	if s.status != 143 || s.opening || !s.receiptKept {
		t.Fatal("canceling before the first opening lost the original receipt")
	}

	c.assertNoCall(t, s.reason, `^systemctl (stop|restart|unmask) `)
	c.assertHeldPair(t)
}

func assertCancelAfterRestart(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	if s.status != 143 || s.opening {
		t.Fatalf("a cancellation right after the start was missed: status=%d opening=%v", s.status, s.opening)
	}

	c.assertOrder(t, s.reason, "systemctl start hololive-alarm-worker.service",
		"guard check hololive-alarm-worker.service", "systemctl stop hololive-alarm-worker.service", "guard seal hololive-alarm-worker.service")
	c.assertNoCall(t, s.reason, `^systemctl unmask hololive-api.service$`)
	c.assertHeldPair(t)
	succeeds(t, "explicit resume after a verified re-stop", c.resume(t, "success", s.apiRelease, s.workerRelease))

	if c.countCalls(t, "systemctl start hololive-db-migrate.service") != 1 {
		t.Fatal("the migration ran again after a re-stop")
	}
}

func assertFailedClientReheld(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	c.assertOrder(t, s.reason, "systemctl start hololive-alarm-worker.service", "guard arm hololive-alarm-worker.service",
		"journal start hololive-alarm-worker.service 71", "guard check hololive-alarm-worker.service",
		"systemctl stop hololive-alarm-worker.service", "guard seal hololive-alarm-worker.service")
	c.assertHeldPair(t)

	if s.reason == "client_failed_pending" && !exists(c.state("hololive-alarm-worker/poll")) {
		t.Fatal("the pending original job was not observed after the guard")
	}

	if !s.opening || !s.receiptKept {
		t.Fatalf("%s: a failed request published completion", s.reason)
	}

	c.assertNoCall(t, s.reason, `^(pair verify-live |systemctl unmask hololive-api.service)`)
}

func assertUnverifiedGenerationKept(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	if s.reason == interruptedClient && s.status != 143 {
		t.Fatal("the interrupted client signal was missed")
	}

	if !s.opening || !s.workerGuarded || s.workerActive != yes {
		t.Fatalf("%s: an unverified generation was adopted", s.reason)
	}

	c.assertNoCall(t, s.reason, `^systemctl (stop |unmask hololive-api.service)`)
}

func assertReholdWithoutEvidence(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	if !s.opening || !s.workerGuarded || s.workerActive != no {
		t.Fatal("an evidence write failure skipped the verified writer stop")
	}

	c.assertNoCall(t, s.reason, `^(guard seal |systemctl unmask hololive-api.service)`)
}

func assertUnmaskUndone(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	c.assertOrder(t, s.reason, "systemctl unmask hololive-api.service", "guard undo-unmask hololive-api.service", "systemctl stop hololive-alarm-worker.service")

	if s.opening {
		t.Fatal("an incomplete opening remained after restoring the owned mask")
	}

	c.assertHeldPair(t)
}

func assertResidualCIDHeld(t *testing.T, c *deployCase, s openingState) {
	t.Helper()

	c.assertNoCall(t, s.reason, `^guard undo-unmask hololive-api.service$`)

	if !s.opening || !exists(c.state("hololive-api/guard")) || s.workerActive != no {
		t.Fatal("a residual original CID was ignored or the peer stop was skipped")
	}
}

func TestAbnormalReholdNeverConsumesTheReceipt(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)
	c.clearCalls(t)
	fails(t, "abnormal rehold", c.resume(t, "failed_rehold", apiRelease, workerRelease))
	c.assertNoCall(t, "rehold", `^((guard (arm|check|seal)|systemctl (stop|start|restart|unmask)) hololive-api.service)$`)

	terminals, err := filepath.Glob(filepath.Join(workerRelease, "rehold.*", "terminal"))
	if err != nil || len(terminals) == 0 || !hasLine(t, terminals[0], "ExecMainStatus=143") {
		t.Fatalf("the original evidence at re-stop was lost: %v", terminals)
	}

	if !isDir(filepath.Join(apiRelease, "opening")) || !exists(c.state("hololive-alarm-worker/guard")) {
		t.Fatal("an incomplete re-stop was accepted")
	}

	c.clearCalls(t)
	fails(t, "resume after a failed re-stop", c.resume(t, "success", apiRelease, workerRelease))
	c.assertNoCall(t, "rehold", `^(writer-sql |podman exec |pair activate |systemctl (start|restart|unmask))`)
}

func TestCardWindowRefusesInfrastructureDrift(t *testing.T) {
	t.Parallel()

	c := coordinatedCase(t)
	apiRelease, workerRelease := c.quiesce(t)
	put(t, c.state("pg-started"), "1794960722000000000\n")
	c.clearCalls(t)
	fails(t, "PostgreSQL restart with the same image", c.resume(t, "success", apiRelease, workerRelease))
	c.assertNoCall(t, "pg", `^(writer-sql |pair activate |systemctl (unmask|start|restart))`)
	c.assertHeldPair(t)

	put(t, c.state("pg-started"), "1794960062300000000\n")
	c.clearCalls(t)
	fails(t, "remaining business-role connections", c.resume(t, "idle_frontend", apiRelease, workerRelease))
	c.assertNoCall(t, "frontend", `^(systemctl (start hololive-db-migrate|unmask|restart)|pair activate )`)
	c.assertHeldPair(t)

	single := newCase(t, api, false, revision)
	put(t, single.state("running-endpoint"), "POSTGRES_HOST=other-pooler\nPOSTGRES_PORT=6000\n")

	r := single.deploy(t, "success")
	fails(t, "single endpoint change", r)
	single.assertOutput(t, r, "single-writer endpoint change refused")
	single.assertNoCall(t, "single", `^(pair |systemctl (start|restart|daemon-reload))`)

	if exists(single.current("artifacts")) {
		t.Fatal("a refused single endpoint change created a release")
	}
}

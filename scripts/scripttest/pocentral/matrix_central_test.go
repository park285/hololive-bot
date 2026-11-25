package pocentral

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const managerClient = "import os,signal,socket,sys\n" +
	"with socket.socket(socket.AF_UNIX) as client:\n" +
	" client.connect(sys.argv[1]); client.sendall(sys.argv[2].encode()); assert client.recv(32)==b'queued'\n" +
	"os.kill(os.getpid(),signal.SIGKILL)\n"

func (m *matrixCase) startManager() (alive func() bool, stop func()) {
	m.t.Helper()

	var lc net.ListenConfig

	listener, err := lc.Listen(m.t.Context(), "unix", m.p("manager.sock"))
	if err != nil {
		m.t.Fatalf("listen manager socket: %v", err)
	}

	unix, ok := listener.(*net.UnixListener)
	if !ok {
		m.t.Fatalf("manager socket is %T", listener)
	}

	release, done := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				m.t.Errorf("close manager socket: %v", err)
			}
		}()

		if err := unix.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
			m.t.Errorf("manager deadline: %v", err)

			return
		}

		client, err := listener.Accept()
		if err != nil {
			return
		}

		buf := make([]byte, 256)
		n, readErr := client.Read(buf)
		recordErr := os.WriteFile(m.p("manager-job"), buf[:n], 0o600)
		_, writeErr := client.Write([]byte("queued"))

		if err := errors.Join(readErr, recordErr, writeErr, client.Close()); err != nil {
			m.t.Errorf("manager exchange: %v", err)
		}

		select {
		case <-release:
		case <-time.After(10 * time.Second):
		}
	}()

	m.wr(m.p("manager-client.py"), managerClient)

	return func() bool {
			select {
			case <-done:
				return false
			default:
				return true
			}
		}, func() {
			close(release)
			<-done
		}
}

func (m *matrixCase) runCentral() {
	var r harness.Result

	if m.name == "client_lost_pending" {
		alive, stop := m.startManager()

		r = run(m.t, m.body, m.state, m.name)

		managerAlive := alive()

		stop()
		m.require(managerAlive && m.isFile(m.p("manager-job")), "client exit does not settle manager job", r)
	} else {
		r = run(m.t, m.body, m.state, m.name)
	}

	m.require(r.ExitCode == m.expect.status, "original status", r)

	calls := m.calls()
	changed := mutated(calls)
	restarts := withPrefix(calls, "systemctl restart ")
	endpointBlocked := m.name == "transition_error_log" || m.name == "transition_wrong_endpoint"

	if endpointBlocked {
		m.require(m.rd(m.p("collector.running")) == stFalse && m.rd(m.p("issuer.running")) == stFalse, "both services stopped", r)
		m.require(m.isSymlink(m.p("system-units/"+m.issuerUnit)) && m.isSymlink(m.p("system-units/"+m.collectorUnit)) &&
			m.rd(m.p("po-"+m.slot+".hold/quiesced")) == guardedHold, "prohibited restore keeps writers durably held", r)
		m.require(exists(filepath.Join(m.backup, "snapshot-complete")) && exists(filepath.Join(m.backup, "collector.endpoint")), "recovery records retained", r)
		m.require(!activatesPrevious(calls), "previous pair never activated", r)
	}

	if slices.Contains([]string{scRollbackCross, "rollback_masked_endpoint", "rollback_missing_endpoint"}, m.name) {
		m.require(!changed, "manual cross-endpoint rollback has no runtime mutations", r)
	}

	if strings.HasPrefix(m.name, "capacity_") || m.name == "rollback_capacity_exhausted" {
		m.require(!changed, "capacity rejection has no runtime mutations", r)

		if m.isRollback {
			m.require(exists(filepath.Join(m.previousPair, "profile.json")) && m.rd(m.p("units/"+container(m.env.slots, m.slot)+".container")) == m.installed,
				"rollback recovery and installed unit retained", r)
		} else {
			m.require(!exists(m.backup), "capacity rejected before snapshot", r)
		}
	}

	expectedWarning := endpointBlocked || slices.Contains([]string{"restore_failure", scActivation, "client_lost_pending", "manager_probe_failure"}, m.name)
	m.require(strings.Contains(r.Stderr, "automatic restoration failed") == expectedWarning, "restoration warning", r)
	m.requireCentralOutcome(r, calls, changed, restarts)
	m.requireCentralRuntime(r, calls, restarts, endpointBlocked)
}

func (m *matrixCase) requireCentralOutcome(r harness.Result, calls []string, changed bool, restarts []string) {
	switch {
	case m.name == "client_lost_pending" || m.name == "manager_probe_failure":
		m.require(strings.Contains(r.Stderr, "outcome_unknown"), "pending/unknown outcome is explicit", r)
		m.require(strings.TrimSpace(m.rd(filepath.Join(m.backup, "cutover-outcome"))) == "outcome_unknown", "outcome receipt retained", r)
		m.require(!activatesPrevious(calls), "no previous pair activation", r)
		m.require(len(restarts) == 1, "no restoration restart while manager outcome is unknown", r)
	case slices.Contains([]string{"missing_receipt", "collector_stopped", "inventory_failure", "unadmitted_deploy", "foreign_card_pair"}, m.name):
		m.require(!exists(m.backup) && !changed, "no partial backup or runtime change", r)
	case m.name == "rollback_stale":
		m.require(!changed, "copied release receipt is not the active owner", r)
	case m.isCheck:
		m.require(!changed, "check has no mutations", r)
		m.require(strings.Contains(r.Stdout, "completion verified") == (m.expect.status == 0), "completion output requires the candidate loaded profile", r)
	case slices.Contains([]string{scCandidateCheck, "absent_candidate_check_failure", scActivation, scDatabaseQuery}, m.name):
		m.require(len(restarts) == 0, "no unit restart", r)
		m.require(m.rd(m.imageFile(collectorImage(m)+":prod")) == stOld, "collector tag baseline", r)
		m.require(exists(m.imageFile(issuerImage(m)+":prod")) != m.issuerAbsent, "issuer tag baseline", r)
		m.require(m.rd(m.p("dropin")) == "initial", "drop-in unchanged", r)
		m.require(m.rd(m.imageFile(collectorImage(m)+":candidate")) == stNew, "candidate retained for inspection", r)
	}
}

func (m *matrixCase) requireCentralRuntime(r harness.Result, calls, restarts []string, endpointBlocked bool) {
	promoted := slices.Contains([]string{scSuccess, "absent_success", "transition_success"}, m.name)

	if promoted {
		m.require(slices.Equal(restarts, prefixed("systemctl restart ", m.issuerUnit, m.collectorUnit)), "issuer-first restart order", r)
		m.require(slices.Contains(calls, "lease-proof target") == (m.slot == "c"), "lease proof only on central", r)
		m.require(strings.TrimSpace(m.rd(m.p("dropin"))) == "candidate", "candidate drop-in active", r)
		m.require(sourceRevision(m.t, filepath.Join(m.livePO, rootfsManifest)) == newRevision, "issuer receipt promoted", r)

		reload := slices.Index(calls, "systemctl daemon-reload")
		m.require(reload >= 0 && reload < slices.Index(calls, "systemctl restart "+m.issuerUnit), "drop-in reloaded before restart", r)
	}

	if m.expect.restored {
		m.require(strings.TrimSpace(m.rd(m.p("dropin"))) == "previous", "previous drop-in restored", r)
		m.require(m.rd(m.imageFile(collectorImage(m)+":prod")) == stOld && m.rd(m.imageFile(issuerImage(m)+":prod")) == stOld, "previous tags restored", r)
		m.require(sourceRevision(m.t, filepath.Join(m.livePO, rootfsManifest)) == oldRevision, "previous issuer receipt restored", r)
	}

	if m.name == "restore_failure" {
		m.require(m.rd(m.p("collector.runtime")) == stOld, "failed restore leaves the unrestarted collector", r)
	}

	if m.name == opRollback {
		releaseDir := filepath.Join(m.current, "artifacts/worker-profile/releases", filepath.Base(m.backup), m.service)
		unit := container(m.env.slots, m.slot) + ".container"
		m.require(m.rd(m.p("units/"+unit)) == m.previous, "previous base unit restored", r)
		m.require(m.rd(filepath.Join(releaseDir, "rollback-replaced-"+unit)) == m.installed, "replaced base unit retained", r)
		m.require(slices.Contains(calls, "systemctl daemon-reload"), "units reloaded", r)
	}

	expected := stOld

	switch {
	case endpointBlocked:
		expected = "absent"
	case m.isCheck || m.isRollback && !m.expect.restored || promoted:
		expected = stNew
	}

	m.require(m.rd(m.p("collector.runtime")) == expected, "collector runtime", r)

	issuer := expected

	switch m.name {
	case "restore_failure", "manager_probe_failure":
		issuer = stNew
	case "absent_candidate_check_failure":
		issuer = "absent"
	}

	m.require(m.rd(m.p("issuer.runtime")) == issuer, "issuer runtime", r)
}

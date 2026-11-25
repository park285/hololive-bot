package pocentral

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type maintenanceRun struct {
	*matrixCase

	scenario, hold, release       string
	unitPath, candidateDefinition string
	overlay, changedOverlay       string
	masks, guards                 []string
}

var failQuiesce = []string{
	"mask_failure", "stop_failure", "stop_other_invocation", "stop_missing_job", "stop_stale_job",
	"stop_peer_restarted", "mask_race", "guard_failure", "guard_check_failure", scForeignGuard, "arm_collector_profile",
	"stop_control_pid", "collector_143", scCollected143, "died_duplicate", "died_other_cid", "died_stale",
	"stop_terminal_invocation", "stop_leftover_id",
}

func (m *matrixCase) runMaintenance() {
	s := &maintenanceRun{
		matrixCase: m, scenario: strings.TrimPrefix(m.name, "maintenance_"), hold: m.p("po-" + m.slot + ".hold"),
		release: filepath.Join(m.current, "artifacts/worker-profile/releases", filepath.Base(m.backup), m.service),
	}

	s.masks = []string{m.p("system-units/" + m.issuerUnit), m.p("system-units/" + m.collectorUnit)}
	s.guards = []string{s.masks[0] + ".guard", s.masks[1] + ".guard"}

	m.wr(m.p("source.profile"), "reviewed candidate profile\n")

	if m.slot == "c" {
		m.wr(filepath.Join(m.current, ".source-revision"), oldRevision)
	}

	if s.refusedBeforeSnapshot() {
		return
	}

	if !s.quiesce() {
		return
	}

	s.installCandidate()

	if s.scenario == scRollbackCross || strings.HasPrefix(s.scenario, "rollback_") {
		s.rollback()
	} else {
		s.resume()
	}
}

func (s *maintenanceRun) assertNewManagerBlocks() {
	m := s.matrixCase

	for _, unit := range []string{m.issuerUnit, m.collectorUnit} {
		attempted := run(m.t, m.manager+"\nsystemctl start "+unit+"\n", m.state, "maintenance_boot")
		m.require(attempted.ExitCode == 8, "persistent admission denied after reboot", attempted)
	}
}

func (s *maintenanceRun) assertHeld(r harness.Result) {
	m := s.matrixCase
	m.require(m.isDir(s.hold) && strings.TrimSpace(m.rd(filepath.Join(s.hold, "backup"))) == m.backup, "hold ownership retained", r)

	for _, mask := range s.masks {
		target, err := os.Readlink(mask)
		m.require(err == nil && target == "/dev/null", "persistent masks", r)
	}

	m.require(m.rd(m.p("issuer.running")) == stFalse && m.rd(m.p("collector.running")) == stFalse, "writers inactive", r)
}

func (m *matrixCase) isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

func (s *maintenanceRun) refusedBeforeSnapshot() bool {
	m := s.matrixCase

	switch s.scenario {
	case "operator_mask":
		harness.Symlink(m.t, "/dev/null", s.masks[0])

		r := m.invoke(modeQuiesce, "")
		m.require(r.ExitCode != 0 && !exists(m.backup) && !exists(s.hold), "refuse unrelated mask before snapshot", r)
		m.require(m.isSymlink(s.masks[0]) && !mutated(m.calls()), "unrelated mask untouched", r)

		return true
	case "lock_busy":
		lock := m.p("po-" + m.slot + ".lock")

		held, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- 테스트 임시 상태 디렉터리 안의 슬롯 잠금 파일이다.
		if err != nil {
			m.t.Fatalf("open lock: %v", err)
		}

		defer func() {
			if err := held.Close(); err != nil {
				m.t.Errorf("close lock: %v", err)
			}
		}()

		if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			m.t.Fatalf("flock: %v", err)
		}

		r := m.invoke(modeQuiesce, "")
		m.require(r.ExitCode != 0 && !exists(m.backup) && len(m.calls()) == 0, "competing operation rejected by slot lock", r)

		return true
	}

	return false
}

func (s *maintenanceRun) quiesce() bool {
	m := s.matrixCase

	if s.scenario == scForeignGuard {
		m.wr(s.guards[1], "operator guard\n")
	}

	if m.slot == "c" && s.scenario == scSuccess {
		m.touch(m.p("pending-suffix"))
	}

	failure := ""

	if strings.HasPrefix(s.scenario, "load_") || slices.Contains(failQuiesce, s.scenario) {
		failure = s.scenario
	}

	r := m.invoke(modeQuiesce, failure)

	if s.refusedQuiesce(r) {
		return false
	}

	if s.scenario == "collector_143" || s.scenario == scCollected143 {
		s.requireAbnormalStopEvidence(r)
	}

	if slices.Contains(incompleteStops, s.scenario) {
		s.requireIncompleteStop(r)

		return false
	}

	if slices.Contains([]string{"mask_failure", "stop_failure", "stop_control_pid", "stop_leftover_id"}, s.scenario) {
		s.requirePartialStop(r)

		return false
	}

	m.require(r.ExitCode == 0, "original pair quiesced", r)
	s.assertHeld(r)
	s.requireQuiesced(r)

	return true
}

func (s *maintenanceRun) refusedQuiesce(r harness.Result) bool {
	m := s.matrixCase

	switch {
	case s.scenario == "previous_candidate":
		m.require(r.ExitCode != 0 && !exists(s.hold) && len(withPrefix(m.calls(), "podman load ", "systemctl stop ", heldStart)) == 0,
			"wrong original route or retained candidate cannot open the migration window", r)

		return true
	case strings.HasPrefix(s.scenario, "load_"):
		m.require(r.ExitCode != 0 && !exists(s.hold) && len(withPrefix(m.calls(), "mask link ", "systemctl stop ", "systemctl restart ")) == 0,
			"changed live pair rejected before hold", r)

		return true
	case slices.Contains([]string{"guard_failure", "guard_check_failure", scForeignGuard, "arm_collector_profile"}, s.scenario):
		m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) && !exists(filepath.Join(s.hold, "quiesced")) &&
			len(withPrefix(m.calls(), "systemctl stop ", "guard seal ", "systemctl restart ")) == 0,
			"no writer stop without both effective reviewed guards", r)

		if s.scenario == scForeignGuard {
			m.require(m.rd(s.guards[1]) == "operator guard\n" && !m.isSymlink(s.masks[0]) && !m.isSymlink(s.masks[1]),
				"foreign guard not replaced or adopted", r)
		}

		return true
	}

	return false
}

var incompleteStops = []string{
	"stop_other_invocation", "stop_missing_job", "stop_stale_job", "stop_peer_restarted", "mask_race",
	"collector_143", scCollected143, "died_duplicate", "died_stale", "died_other_cid", "stop_terminal_invocation",
}

func (s *maintenanceRun) requireIncompleteStop(r harness.Result) {
	m := s.matrixCase

	if s.scenario == "stop_peer_restarted" || s.scenario == "mask_race" {
		m.require(!slices.Contains(m.calls(), "systemctl stop "+m.collectorUnit), "changed peer not accepted as captured after issuer stop", r)
	}

	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) && !exists(filepath.Join(s.hold, "quiesced")),
		"no receipt for wrong or absent stop job", r)

	before := len(m.calls())

	for _, mode := range []string{modeResume, opRollback} {
		refused := m.invoke(mode, "")
		m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), "incomplete stop cannot release the pair", refused)
	}
}

func (s *maintenanceRun) requireAbnormalStopEvidence(r harness.Result) {
	m := s.matrixCase
	_, outcome, found := strings.Cut(m.rd(filepath.Join(m.backup, "guarded-stop.log")), "unit="+m.collectorUnit+" stop=true invocation="+strings.Repeat("0", 31)+"1")
	first, _, _ := strings.Cut(outcome, "\n")
	marker := "ExecMainStatus=143"

	if s.scenario == scCollected143 {
		marker = "ActiveState=inactive"
	}

	m.require(found && strings.Contains(first, "exit_status=143") && strings.Contains(outcome, "unit="+m.collectorUnit+" unconfirmed") &&
		strings.Contains(outcome, marker) && m.isSymlink(s.masks[1]) && !exists(m.p("collector.exit")),
		"matched manager stop and abnormal collector result survive manager collection", r)
}

func (s *maintenanceRun) requirePartialStop(r harness.Result) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && exists(s.hold) && exists(filepath.Join(m.backup, "snapshot-complete")), "partial failure retains recovery", r)
	m.require(len(withPrefix(m.calls(), "systemctl restart ")) == 0, "no old writer restart", r)
	m.require(m.rd(m.p("issuer.running")) == stFalse && m.isSymlink(s.masks[0]), "issuer stopped first", r)

	collector := stFalse

	if s.scenario == "stop_failure" {
		collector = stTrue
	}

	m.require(m.rd(m.p("collector.running")) == collector, "stop outcome preserved", r)
	m.require(!m.isSymlink(s.masks[1]) && m.isFile(s.guards[1]) && !exists(filepath.Join(s.hold, "quiesced")) && strings.Contains(r.Stderr, unconfirmed),
		"persistent guard retained, no receipt", r)
	s.assertNewManagerBlocks()

	before := len(m.calls())
	rejected := m.invoke(modeResume, "")
	m.require(rejected.ExitCode != 0 && !mutated(m.calls()[before:]), "incomplete hold cannot resume", rejected)
}

func (s *maintenanceRun) requireQuiesced(r harness.Result) {
	m := s.matrixCase

	if s.scenario == scSuccess {
		before := len(m.calls())
		refused := m.invoke(opRollback, "")
		m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), "card transition never issues rollback authority", refused)
	}

	m.require(m.rd(filepath.Join(s.hold, "quiesced")) == guardedHold && !exists(s.guards[0]) && !exists(s.guards[1]),
		"both guards sealed before versioned receipt", r)
	m.require(m.rd(filepath.Join(s.release, "previous/unit.container")) == m.installed && !exists(filepath.Join(s.release, "candidate")),
		"old unit preserved without premature candidate", r)
	m.require(m.rd(m.imageFile(collectorImage(m)+":prod")) == stOld && m.rd(m.p("dropin")) == "initial", "quiesce does not publish candidate", r)

	calls := m.calls()
	stops := withPrefix(calls, "systemctl stop ")
	m.require(slices.Equal(stops, prefixed("systemctl stop ", m.issuerUnit, m.collectorUnit)), "issuer-first stop", r)

	lastArm := max(slices.Index(calls, "guard arm "+m.issuerUnit), slices.Index(calls, "guard arm "+m.collectorUnit))
	firstSeal := min(slices.Index(calls, "guard seal "+m.issuerUnit), slices.Index(calls, "guard seal "+m.collectorUnit))
	m.require(lastArm >= 0 && firstSeal >= 0 && lastArm < slices.Index(calls, stops[0]) && slices.Index(calls, stops[len(stops)-1]) < firstSeal,
		"both guards before stop and both stop outcomes before mask reload", r)

	m.touch(m.p("db.stopped"))

	before := len(m.calls())
	checked := m.invoke("check", "")
	competed := m.invoke(modeQuiesce, "")
	m.require(checked.ExitCode != 0 && competed.ExitCode != 0 && !mutated(m.calls()[before:]), "held state is not completed or replaceable", checked)

	if s.scenario == "reboot" {
		harness.Remove(m.t, m.p("issuer.exit"))
		s.assertNewManagerBlocks()
		s.assertHeld(r)
	}
}

func (s *maintenanceRun) installCandidate() {
	m := s.matrixCase

	s.candidateDefinition = m.installed
	s.unitPath = m.p("units/" + container(m.env.slots, m.slot) + ".container")
	m.wr(s.unitPath, s.candidateDefinition)

	if m.slot == "c" {
		m.wr(filepath.Join(m.current, ".source-revision"), newRevision)
	} else {
		m.wr(s.unitPath, m.rd(filepath.Join(m.staging, m.bUnitSource)))
		require(m.t, m.rd(s.unitPath) == s.candidateDefinition && !exists(filepath.Join(m.current, ".source-revision")),
			"b: actual staged candidate installed without source marker", harness.Result{})
	}

	harness.Remove(m.t, m.p("db.stopped"))
	s.damage()
}

func (s *maintenanceRun) damage() {
	m := s.matrixCase

	switch s.scenario {
	case "other_exit":
		m.wr(m.p("issuer.exit"), "137\n")
	case scOtherInvocation:
		m.wr(m.p("issuer.invocation"), strings.Repeat("0", 31)+"2")
	case "missing_stop_receipt":
		harness.Remove(m.t, filepath.Join(s.hold, "quiesced"))
	case "blank_receipt":
		m.wr(filepath.Join(s.hold, "quiesced"), "")

		before := len(m.calls())
		refused := m.invoke(opRollback, "")
		m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), "a blank stop receipt cannot release masks on rollback", refused)
	case "leftover_container":
		m.touch(m.p("collector.leftover"))
	case "inventory_unavailable":
		m.touch(m.p("inventory-unavailable"))
	}

	if s.scenario == "capacity_exhausted" {
		m.wr(m.p("compose.env"), "COLLECTOR_POSTGRES_POOL_MAX_CONNS=8\n")
	}

	switch {
	case s.scenario == "wrong_endpoint" || strings.HasPrefix(s.scenario, "rollback_"):
		m.wr(s.unitPath, strings.NewReplacer(primaryHost, otherPooler, "POSTGRES_PORT=5432", "POSTGRES_PORT=6000", "POSTGRES_PORT=5433", "POSTGRES_PORT=6001").Replace(m.installed))
	case s.scenario == "origin_receipt_changed":
		host, port := m.foreignEndpoint()
		m.wr(filepath.Join(m.backup, "collector.endpoint"), host+"|"+port+"\n")
	case s.scenario == "copied_hold_receipt":
		receipt := filepath.Join(s.hold, "card-candidate")
		harness.Remove(m.t, receipt)
		m.wr(receipt, m.rd(filepath.Join(m.backup, "card-candidate")))
	case s.scenario == "lost_seal":
		harness.Remove(m.t, filepath.Join(m.backup, "transport.sha256"))
	case s.scenario == "seal_changed":
		m.wr(filepath.Join(m.staging, "transport-files.sha256"), "other transport\n")
	}

	s.damageInputs()
}

func (s *maintenanceRun) damageInputs() {
	m := s.matrixCase

	switch s.scenario {
	case "source_changed":
		if m.slot == "c" {
			m.wr(filepath.Join(m.current, ".source-revision"), strings.Repeat("c", 40))
		} else {
			staged := filepath.Join(m.staging, m.bUnitSource)
			m.wr(staged, m.rd(staged)+"# unreviewed source\n")
		}
	case "installed_unit_changed":
		m.wr(s.unitPath, s.candidateDefinition+"Environment=EXTRA=changed\n")
	case "helper_changed":
		helper := filepath.Join(m.staging, "scripts/deploy/lib/compose-env.sh")
		m.wr(helper, m.rd(helper)+"\ntouch \""+m.state+"/unreviewed-helper-ran\"\n")
	case "migration_changed":
		m.wr(filepath.Join(m.staging, "hololive/hololive-api/scripts/migrations/001_schema_epoch4_baseline.sql"), "-- changed\n")
	case "source_manifest_changed":
		m.wr(filepath.Join(m.staging, "source-files.sha256"), sha256Hex(m.rd(s.unitPath))+"  "+m.bUnitSource+"\n")
	case "identity_changed":
		m.wr(m.imageFile(collectorImage(m)+":candidate"), stOld)
	case "inputs_changed":
		m.wr(filepath.Join(m.staging, "collector-image-id"), oldID)
	case scForeignHold:
		m.wr(filepath.Join(s.hold, "backup"), m.backup+"-foreign\n")
	case "mask_replaced":
		harness.Remove(m.t, s.masks[0])
		harness.Symlink(m.t, "/dev/null", s.masks[0])
	case "incomplete_hold":
		harness.Remove(m.t, s.masks[1])
	case "candidate_unit_changed", "candidate_profile_changed", "candidate_image_overlay_changed", "candidate_endpoint_overlay_changed":
		failed := m.invoke(modeResume, scCandidateCheck)
		m.require(failed.ExitCode != 0, "retain candidate after validation failure", failed)
		s.assertHeld(failed)

		switch s.scenario {
		case "candidate_unit_changed":
			m.wr(s.unitPath, s.candidateDefinition+"Environment=EXTRA=changed\n")
		case "candidate_profile_changed":
			m.wr(m.p("source.profile"), "unreviewed profile\n")
		default:
			s.overlay = filepath.Join(s.release, "candidate/50-worker-profile.conf")

			extra := "Environment=POSTGRES_PORT=5432\n"

			if s.scenario == "candidate_image_overlay_changed" {
				extra = "Image=unreviewed\n"
			}

			s.changedOverlay = m.rd(s.overlay) + extra
			m.wr(s.overlay, s.changedOverlay)
		}
	}
}

func (s *maintenanceRun) rollback() {
	m := s.matrixCase
	before := len(m.calls())
	r := m.invoke(opRollback, "")
	m.require(r.ExitCode != 0 && !mutated(m.calls()[before:]), "cross-endpoint rollback refused under hold", r)
	s.assertHeld(r)

	if s.scenario == scRollbackCross {
		return
	}

	m.wr(s.unitPath, m.installed)

	failedRestore := slices.Contains([]string{"rollback_image", "rollback_profile", "rollback_unhealthy", "rollback_issuer_failure", "rollback_replaced"}, s.scenario)
	failure := ""

	if failedRestore {
		failure = s.scenario
	}

	before = len(m.calls())
	r = m.invoke(opRollback, failure)

	if !failedRestore {
		m.require(r.ExitCode == 0 && !exists(s.hold) && !m.isSymlink(s.masks[0]) && !m.isSymlink(s.masks[1]), "coordinated rollback releases owned masks", r)
		m.require(m.runtimes(stOld), "rollback restores original pair", r)
		m.require(m.rd(s.unitPath) == m.installed && m.rd(filepath.Join(s.release, "previous/unit.container")) == m.installed,
			"coordinated rollback retains the exact previous unit", r)

		return
	}

	s.requireFailedRollback(r, before)
}

func (s *maintenanceRun) requireFailedRollback(r harness.Result, before int) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && !strings.Contains(r.Stdout, "previous collector/issuer restored"), "ready does not hide the failed restored runtime check", r)

	if s.scenario == "rollback_replaced" {
		m.require(slices.Equal(withPrefix(m.calls()[before:], "systemctl stop "), []string{"systemctl stop " + m.issuerUnit}) && m.isFile(s.guards[1]) &&
			m.rd(m.p("collector.running")) == stTrue && !exists(filepath.Join(s.hold, "quiesced")) && strings.Contains(r.Stderr, unconfirmed),
			"held rollback never stops replacement or certifies it as the original", r)
		s.assertNewManagerBlocks()

		return
	}

	s.assertHeld(r)

	reopened := []string{m.issuerUnit, m.collectorUnit}

	if s.scenario == "rollback_issuer_failure" {
		reopened = reopened[:1]
	}

	m.require(slices.Equal(withPrefix(m.calls()[before:], "guard arm "), prefixed("guard arm ", reopened...)) &&
		m.isFile(filepath.Join(s.hold, "quiesced")) && m.rd(filepath.Join(s.hold, "quiesced")) == guardedHold,
		"failed manual rollback re-holds only opened units", r)
}

func (s *maintenanceRun) resume() {
	m := s.matrixCase
	before := len(m.calls())

	if m.slot == "c" && s.scenario == scSuccess {
		r := harness.Result{}
		m.require(slices.Contains(m.calls(), "lease-proof applied") && !slices.Contains(m.calls(), "lease-proof target"),
			"a pending target does not prevent quiescing the applied head", r)

		blocked := m.invoke(modeResume, "")
		actions := m.calls()[before:]
		m.require(blocked.ExitCode != 0 && slices.Contains(actions, "lease-proof target") &&
			len(withPrefix(actions, heldStart, "systemctl restart ", "systemctl unmask ", "pair activate ")) == 0,
			"pending target cannot activate candidate or release either writer", blocked)
		s.assertHeld(blocked)
		harness.Remove(m.t, m.p("pending-suffix"))
	}

	failure := ""

	if slices.Contains(append([]string{
		scActivation, scCandidateCheck, scDatabaseQuery, scSubstitution,
		scErrorLog, "unmask_failure", "signal_TERM", scErrorLogSeal,
	}, recoveryCases...), s.scenario) {
		failure = s.scenario
	}

	r := m.invoke(modeResume, failure)
	success := s.scenario == scSuccess || s.scenario == "reboot"
	m.require((r.ExitCode == 0) == success, "resume outcome", r)

	subsequent := m.calls()[before:]
	m.require(len(withPrefix(subsequent, "podman load ", "pair capture ")) == 0, "resume neither reloads nor recaptures", r)

	if success {
		s.requireResumed(r, subsequent)

		return
	}

	m.require(exists(s.hold) && !activatesPrevious(subsequent), "no automatic direct restoration", r)
	m.require(m.rd(filepath.Join(s.release, "previous/unit.container")) == m.installed && !exists(m.p("unreviewed-helper-ran")),
		"recovery unit retained and unverified helper never executed", r)

	if slices.Contains(recoveryCases, s.scenario) || s.scenario == "signal_TERM" {
		s.requireRecovery(r, subsequent)
		s.assertNewManagerBlocks()

		return
	}

	s.requireFailedResume(r, subsequent)
}

func (s *maintenanceRun) requireResumed(r harness.Result, subsequent []string) {
	m := s.matrixCase
	m.require(!exists(s.hold) && !m.isSymlink(s.masks[0]) && !m.isSymlink(s.masks[1]), "successful resume releases ownership", r)
	m.require(m.rd(filepath.Join(s.release, "candidate/unit.container")) == s.candidateDefinition, "candidate uses actual installed unit", r)
	m.require(slices.Equal(withPrefix(subsequent, heldStart, "systemctl restart "), prefixed(heldStart, m.issuerUnit, m.collectorUnit)), "issuer-first resume", r)
	m.require(m.runtimes(stNew), "candidate pair running", r)

	checked := m.invoke("check", "")
	m.require(checked.ExitCode == 0, "completion check after resume", checked)

	before := len(m.calls())
	refused := m.invoke(opRollback, "")
	m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), "accepted card transition cannot resurrect the previous publisher", refused)
}

var signalStatus = map[string]int{"TERM": 143, "INT": 130, "HUP": 129}

func (s *maintenanceRun) requireRecovery(r harness.Result, subsequent []string) {
	stops := withPrefix(subsequent, "systemctl stop ")
	starts := withPrefix(subsequent, heldStart, "systemctl restart ")

	switch {
	case strings.HasPrefix(s.scenario, "receipt_signal") || strings.HasPrefix(s.scenario, "unmask_reload"):
		s.requirePreStartRestore(r, subsequent, stops, starts)
	case strings.HasPrefix(s.scenario, "signal_"):
		name := s.scenario[strings.LastIndex(s.scenario, "_")+1:]
		s.require(r.ExitCode == signalStatus[name] && slices.Equal(starts, []string{heldStart + s.issuerUnit}) &&
			slices.Equal(stops, []string{"systemctl stop " + s.issuerUnit}) && s.rd(filepath.Join(s.hold, "quiesced")) == guardedHold,
			"cancel stops only the one launched job", r)
		s.assertHeld(r)
	default:
		s.requireProvenStops(r, stops, starts)
	}
}

func (s *maintenanceRun) requirePreStartRestore(r harness.Result, subsequent, stops, starts []string) {
	m := s.matrixCase
	receipt := filepath.Join(s.hold, "quiesced")
	intact := s.scenario == "receipt_signal" || s.scenario == "unmask_reload"

	var undo []string

	if s.scenario == "unmask_reload" {
		undo = []string{"guard undo-unmask " + m.issuerUnit}
	}

	m.require(len(stops) == 0 && len(starts) == 0 && slices.Equal(withPrefix(subsequent, "guard undo-unmask "), undo) && exists(receipt) == intact,
		"only verified pre-start masks can be restored", r)

	if strings.HasPrefix(s.scenario, "receipt_signal") {
		m.require(r.ExitCode == 143 && len(withPrefix(subsequent, "systemctl unmask ")) == 0, "cancellation during receipt invalidation stays before unmask", r)
	}

	if intact {
		s.assertHeld(r)
		m.require(m.rd(receipt) == guardedHold, "prior managed hold retained", r)
	}
}

func (s *maintenanceRun) provenStopExpectation() (started, expectedStops []string, issuerRunning, collectorRunning string) {
	m := s.matrixCase
	bothStarted := strings.HasPrefix(s.scenario, "error_log_") || s.scenario == "late_notify_client"
	issuerStopped := bothStarted || s.scenario == "peer_probe_failure" || s.scenario == "client_unknown"

	if issuerStopped {
		expectedStops = append(expectedStops, m.issuerUnit)
	}

	if s.scenario == "error_log_evidence_failure" || s.scenario == "late_notify_client" {
		expectedStops = append(expectedStops, m.collectorUnit)
	}

	started = []string{m.issuerUnit}

	if bothStarted {
		started = append(started, m.collectorUnit)
	}

	issuerRunning, collectorRunning = stTrue, stFalse

	if issuerStopped {
		issuerRunning = stFalse
	}

	if bothStarted && !slices.Contains(expectedStops, m.collectorUnit) {
		collectorRunning = stTrue
	}

	return started, expectedStops, issuerRunning, collectorRunning
}

func (s *maintenanceRun) requireProvenStops(r harness.Result, stops, starts []string) {
	m := s.matrixCase
	started, expectedStops, issuerRunning, collectorRunning := s.provenStopExpectation()

	m.require(slices.Equal(starts, prefixed(heldStart, started...)) && slices.Equal(stops, prefixed("systemctl stop ", expectedStops...)) &&
		m.rd(m.p("issuer.running")) == issuerRunning && m.rd(m.p("collector.running")) == collectorRunning &&
		!exists(filepath.Join(s.hold, "quiesced")) && strings.Contains(r.Stderr, unconfirmed),
		"only independently proven first generations stopped; no receipt", r)

	if s.scenario == "late_notify_client" {
		m.require(m.isFile(m.p("notify.246s")) && !exists(m.p("notify.pending")), "tracked collector job completed after 245s within its own limit", r)
	}
}

func (s *maintenanceRun) requireFailedResume(r harness.Result, subsequent []string) {
	m := s.matrixCase

	if s.scenario == scSubstitution {
		m.require(strings.Contains(r.Stderr, unconfirmed) && !exists(filepath.Join(s.hold, "quiesced")) &&
			!m.isSymlink(s.masks[0]) && m.isFile(s.guards[0]) && m.isSymlink(s.masks[1]) &&
			m.rd(m.p("issuer.running")) == stFalse && m.rd(m.p("collector.running")) == stFalse &&
			len(withPrefix(subsequent, "systemctl stop ")) == 0,
			"failed start with exit 143 is guarded, never adopted as a managed stop", r)
	}

	if slices.Contains([]string{scSubstitution, "unmask_failure", scErrorLog, scErrorLogSeal}, s.scenario) {
		s.requireReopenedGuards(r, subsequent)
	}

	s.requireRefusedResume(r, subsequent)
}

func (s *maintenanceRun) requireReopenedGuards(r harness.Result, subsequent []string) {
	m := s.matrixCase

	reopened := []string{m.issuerUnit}

	if strings.HasPrefix(s.scenario, scErrorLog) {
		reopened = append(reopened, m.collectorUnit)
	}

	m.require(slices.Equal(withPrefix(subsequent, "guard arm "), prefixed("guard arm ", reopened...)), "only reopened units acquire fresh admission guards", r)

	switch s.scenario {
	case scErrorLogSeal:
		m.require(!exists(filepath.Join(s.hold, "quiesced")) && m.isFile(s.guards[1]) && !m.isSymlink(s.masks[1]),
			"incomplete protective seal does not reissue a receipt", r)
	case scSubstitution:
	default:
		m.require(m.rd(filepath.Join(s.hold, "quiesced")) == guardedHold, "protective stop outcomes confirmed", r)
	}

	s.assertNewManagerBlocks()
}

func (s *maintenanceRun) requireRefusedResume(r harness.Result, subsequent []string) {
	m := s.matrixCase

	if s.scenario == scDatabaseQuery {
		m.require(!mutated(subsequent), "failed SQL before activation retains the hold", r)
	}

	if !slices.Contains([]string{scForeignHold, "incomplete_hold", scErrorLogSeal, scSubstitution}, s.scenario) {
		s.assertHeld(r)
	}

	if s.scenario == "candidate_image_overlay_changed" || s.scenario == "candidate_endpoint_overlay_changed" {
		m.require(!mutated(subsequent) && m.rd(s.overlay) == s.changedOverlay, "modified overlay refused without publishing or repairing it", r)
	}

	if strings.HasPrefix(s.scenario, "capacity_") {
		m.require(!mutated(subsequent) && exists(filepath.Join(s.release, "previous/profile.json")), "capacity refusal preserves hold and recovery before effects", r)
	}

	if !slices.Contains([]string{scSubstitution, scErrorLog, "unmask_failure", scErrorLogSeal}, s.scenario) {
		m.require(len(withPrefix(subsequent, "systemctl restart ", "systemctl unmask ")) == 0, "validation fails before unmask or activation", r)
	}
}

package pocentral

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type stagedRun struct {
	*matrixCase

	scenario, hold, expectedActive string
	masks                          []string
}

func (s *stagedRun) masksHeld() bool {
	return s.isSymlink(s.masks[0]) && s.isSymlink(s.masks[1])
}

func (s *stagedRun) bothRunning(state string) bool {
	return s.rd(s.p("collector.running")) == state && s.rd(s.p("issuer.running")) == state
}

func (m *matrixCase) runStaged() {
	m.dropGroupWrite(m.staging)

	s := &stagedRun{matrixCase: m, scenario: strings.TrimPrefix(m.name, "staged_"), hold: m.p("po-b.hold"), expectedActive: m.originPair}
	kernelBoot := m.p("boot-id")
	m.wr(kernelBoot, "c9b38886-755a-444a-9cb8-74cfb379012f\n")
	chmod(m.t, kernelBoot, 0o444)

	m.body = strings.ReplaceAll(m.body, "/proc/sys/kernel/random/boot_id", `"$fixture_root/boot-id"`)
	s.masks = []string{m.p("system-units/" + m.issuerUnit), m.p("system-units/" + m.collectorUnit)}

	generic := run(m.t, m.body, m.state, "generic_deploy")
	m.require(generic.ExitCode != 0 && !mutated(m.calls()), "generic automatic restoration path cannot consume sealed staging", generic)
	s.prepareQuiesce()

	if !s.quiesce() {
		return
	}

	s.damageHold()
	s.resume()
}

func (s *stagedRun) prepareQuiesce() {
	m := s.matrixCase

	if s.scenario == "already_loaded" {
		for _, image := range []string{collectorImage(m), issuerImage(m)} {
			m.wr(m.imageFile(image+":candidate"), stNew)
		}
	}

	if s.scenario == "partial_transport" {
		transport := filepath.Join(m.staging, "transport-files.sha256")
		first, _, _ := strings.Cut(m.rd(transport), "\n")
		m.wr(transport, first+"\n")
		m.wr(m.p("trusted-seal"), sha256Hex(m.rd(transport)))
	}

	switch s.scenario {
	case "archive_drift":
		m.wr(filepath.Join(m.staging, "image.tar"), "substituted archive with same manifest")
	case "origin_stale":
		m.wr(filepath.Join(m.priorBackup, "card-candidate"), strings.ReplaceAll(m.originPair, oldRevision, newRevision))
	case "other_owner":
		other := m.p("card-po-b-20261001T000000Z")
		m.mkdir(other)

		s.expectedActive = m.dirPair(other, oldRevision, oldID)
		m.wr(filepath.Join(other, "card-candidate"), s.expectedActive)
		m.wr(m.active, s.expectedActive)
	case scOtherInvocation:
		m.wr(m.p("collector.invocation"), strings.Repeat("0", 31)+"7\n")
	case "other_cid":
		m.wr(m.p("collector.cid"), "7")
	case "unsafe_stage":
		harness.Symlink(m.t, m.priorBackup, filepath.Join(m.staging, "foreign"))
	case "running_other_endpoint_origin":
		m.touch(m.p("direct-origin"))
	}
}

var unprovenStaging = []string{"archive_drift", "partial_transport", "origin_stale", "other_owner", scOtherInvocation, "other_cid", "unsafe_stage", "running_other_endpoint_origin"}

func (s *stagedRun) quiesce() bool {
	m := s.matrixCase
	preflightFailure := s.scenario == scCandidateCheck || s.scenario == scDatabaseQuery
	failure := ""

	if preflightFailure || s.scenario == "expire_after_stop" {
		failure = s.scenario
	}

	r := m.invoke(modeQuiesce, failure)
	calls := m.calls()

	switch {
	case s.scenario == "expire_after_stop":
		s.requireExpiredStop(r, calls)

		return false
	case preflightFailure:
		s.requirePreflightRefusal(r, calls)

		return false
	case slices.Contains(unprovenStaging, s.scenario):
		m.require(r.ExitCode != 0 && !exists(s.hold) && !exists(m.backup) && s.bothRunning(stTrue) && !mutated(calls) && m.rd(m.active) == s.expectedActive,
			"staged bytes and admitted owner proven before effects", r)

		return false
	}

	m.require(r.ExitCode == 0 && m.rd(filepath.Join(s.hold, "quiesced")) == guardedHold &&
		m.rd(filepath.Join(m.backup, "card-origin")) == m.originPair &&
		strings.TrimSpace(m.rd(filepath.Join(m.backup, "transport.sha256"))) == m.rd(m.p("trusted-seal")),
		"transport proof and prior admitted publisher retained independently", r)

	if s.scenario == "already_loaded" {
		m.require(len(withPrefix(m.calls(), "podman load ")) == 0, "matching candidate images never reloaded", r)
	}

	return true
}

func (s *stagedRun) requireExpiredStop(r harness.Result, calls []string) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) &&
		slices.Equal(withPrefix(calls, "systemctl stop "), []string{"systemctl stop " + m.issuerUnit}) &&
		m.isFile(filepath.Join(s.hold, m.issuerUnit+".stop-cid")) && !exists(filepath.Join(s.hold, "quiesced")) &&
		len(withPrefix(calls, "guard seal ")) == 0 && m.rd(m.active) == m.originPair,
		"settle the submitted issuer stop; no second stop or seal after expiry", r)
}

func (s *stagedRun) requirePreflightRefusal(r harness.Result, calls []string) {
	m := s.matrixCase
	check := "pair check"

	if s.scenario == scDatabaseQuery {
		check = "pair check-database"
	}

	reached := slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, check+" ") && strings.HasSuffix(c, "/candidate") })
	intact := true

	for _, k := range []string{"collector", "issuer"} {
		intact = intact && m.rd(m.p(k+".running")) == stTrue && strings.TrimSpace(m.rd(m.p(k+".invocation"))) == strings.Repeat("0", 31)+"1" &&
			m.rd(m.p(k+".runtime")) == stOld
	}

	m.require(reached && r.ExitCode != 0 && !exists(s.hold) && m.rd(m.active) == m.originPair && intact &&
		len(withPrefix(calls, "guard arm ", "systemctl stop ", heldStart, "pair activate ")) == 0,
		"incompatible profile or database preflight cannot stop the healthy admitted pair", r)
}

func (s *stagedRun) damageHold() {
	m := s.matrixCase

	switch s.scenario {
	case "absent_hold":
		rename(m.t, s.hold, m.p("unowned-hold"))
	case scForeignHold:
		m.wr(filepath.Join(s.hold, "backup"), m.priorBackup)
	case "missing_seal":
		harness.Remove(m.t, filepath.Join(m.backup, "transport.sha256"))
	case "changed_seal":
		m.wr(m.p("trusted-seal"), strings.Repeat("e", 64))
	case "wrong_window":
		approved := m.rd(m.p("approved-origin"))
		m.wr(m.p("approved-origin"), strings.Repeat("9", 32)+approved[32:])
	case "wrong_boot":
		next := m.p("next-boot-id")
		m.wr(next, "28ceceb9-e349-4d7c-b6a7-1f77ee31b4f0\n")
		chmod(m.t, next, 0o444)
		rename(m.t, next, m.p("boot-id"))
	case "wrong_tool":
		m.wr(m.p("approved-origin"), strings.ReplaceAll(m.rd(m.p("approved-origin")), strings.Repeat("0", 39)+"1\n", strings.Repeat("f", 40)+"\n"))
	case "profile_drift":
		profile := filepath.Join(m.current, "artifacts/worker-profile/releases", filepath.Base(m.backup), m.service, "candidate/profile.json")
		m.wr(profile, "substituted valid profile")
		m.wr(m.p("source.profile"), m.rd(profile))
	}
}

func (s *stagedRun) resume() {
	m := s.matrixCase
	before := len(m.calls())
	failure := ""

	if slices.Contains([]string{scErrorLog, "missing_start_record", "old_start_cid", "expire_after_issuer", "expire_after_unmask", "expired_late_notify_client"}, s.scenario) {
		failure = s.scenario
	}

	r := m.invoke(modeResume, failure)
	after := m.calls()[before:]

	if s.resumeRefused(r, after, before) {
		return
	}

	m.require(slices.Equal(withPrefix(after, heldStart, "systemctl restart "), prefixed(heldStart, m.issuerUnit, m.collectorUnit)) && !activatesPrevious(after),
		"only guarded original start requests, no automatic restoration", r)

	switch s.scenario {
	case scSuccess, "already_loaded":
		s.requireActivated(r)
	case "missing_start_record", "old_start_cid":
		s.requireUnconfirmedStart(r, after)
	default:
		s.requireHeldAfterLog(r)
	}
}

func (s *stagedRun) resumeRefused(r harness.Result, after []string, before int) bool {
	m := s.matrixCase

	switch s.scenario {
	case "expired_late_notify_client":
		s.requireExpiredLateNotify(r, after)

		return true
	case "expire_after_unmask":
		s.requireExpiredUnmask(r, after)

		return true
	case "expire_after_issuer":
		s.requireExpiredIssuer(r, after)

		return true
	case "absent_hold", scForeignHold, "missing_seal", "changed_seal", "wrong_window", "wrong_boot", "wrong_tool", "profile_drift":
		m.require(r.ExitCode != 0 && !mutated(after) && s.masksHeld() && m.rd(m.active) == m.originPair,
			"never release masks without seal and owned current hold", r)

		if s.scenario == "wrong_tool" {
			refused := m.invoke(opRollback, "")
			m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), "rollback cannot substitute the staged tool receipt", refused)
		}

		return true
	}

	return false
}

func (s *stagedRun) requireExpiredUnmask(r harness.Result, after []string) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) && m.isSymlink(s.masks[1]) &&
		slices.Equal(withPrefix(after, "systemctl unmask "), []string{"systemctl unmask " + m.issuerUnit}) &&
		len(withPrefix(after, heldStart, "systemctl stop ", "guard arm ")) == 0,
		"an admitted unmask does not authorize a start or rehold after expiry", r)
}

func (s *stagedRun) requireExpiredIssuer(r harness.Result, after []string) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) &&
		slices.Equal(withPrefix(after, heldStart), []string{heldStart + m.issuerUnit}) &&
		m.isFile(filepath.Join(m.backup, "resume-"+m.issuerUnit+".start")) && m.isSymlink(s.masks[1]) &&
		!slices.Contains(after, "systemctl unmask "+m.collectorUnit) && len(withPrefix(after, "systemctl stop ")) == 0,
		"settle issuer start; collector remains masked and no expired rehold is submitted", r)
}

func (s *stagedRun) requireUnconfirmedStart(r harness.Result, after []string) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, unconfirmed) && !exists(filepath.Join(s.hold, "quiesced")) &&
		!exists(filepath.Join(m.backup, "resume-"+m.collectorUnit+".start")) && m.rd(m.p("collector.running")) == stTrue &&
		!slices.Contains(after, "systemctl stop "+m.collectorUnit) && m.rd(m.active) == m.originPair,
		"running collector without original manager record is never stopped or certified", r)
	s.refuses(opRollback, "no rollback from unconfirmed collector start")
}

func (s *stagedRun) requireHeldAfterLog(r harness.Result) {
	m := s.matrixCase
	m.require(r.ExitCode != 0 && strings.Contains(r.Stderr, "[OUTCOME] held:") && s.masksHeld() &&
		m.rd(filepath.Join(s.hold, "quiesced")) == guardedHold && m.rd(m.active) == m.originPair,
		"log failure stops only the admitted start generations; no publisher restored", r)
	s.refuses(modeResume, "repeated post-card activation refused")

	restored := m.invoke(opRollback, "")
	m.require(restored.ExitCode == 0 && !exists(s.hold) && m.rd(m.active) == m.originPair && m.runtimes(stOld),
		"separately authorized confirmed hold may restore the admitted origin, not the pre-card publisher", restored)
}

func (s *stagedRun) refuses(op, message string) {
	m := s.matrixCase
	before := len(m.calls())
	refused := m.invoke(op, "")
	m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), message, refused)
}

var startStamp = regexp.MustCompile(`^@\d+\.\d{6}$`)

func (s *stagedRun) requireExpiredLateNotify(r harness.Result, after []string) {
	m := s.matrixCase
	submitted := strings.Fields(m.rd(filepath.Join(m.backup, "resume-"+m.collectorUnit+".start-request")))
	observed := strings.Fields(m.rd(filepath.Join(m.backup, "resume-"+m.collectorUnit+".start")))
	start := heldStart + m.collectorUnit
	at := slices.Index(after, start)
	m.require(r.ExitCode == 9 && strings.Contains(r.Stderr, unconfirmed) && len(submitted) == 3 && startStamp.MatchString(submitted[0]) &&
		slices.Equal(submitted[1:], []string{"18", "9"}) && len(observed) >= 5 && slices.Equal(observed[:2], submitted[:2]) &&
		slices.Equal(observed[2:5], []string{strings.Repeat("0", 31) + "2", strings.Repeat("0", 63) + "5", strings.Repeat("b", 64)}) &&
		m.isFile(m.p("notify.246s")) && !exists(m.p("notify.pending")) && m.rd(m.p("collector.running")) == stTrue &&
		!exists(filepath.Join(s.hold, "quiesced")) && countOf(after, start) == 1 && at >= 0 && !mutated(after[at+1:]) && m.rd(m.active) == m.originPair,
		"expired client failure retains the submitted and settled job without another writer action", r)
}

func (m *matrixCase) dropGroupWrite(dir string) {
	m.t.Helper()

	root, err := os.OpenRoot(dir)
	if err != nil {
		m.t.Fatalf("open %s: %v", dir, err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			m.t.Errorf("close %s: %v", dir, closeErr)
		}
	}()

	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return err
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return fmt.Errorf("stat %s: %w", path, infoErr)
		}

		if chmodErr := root.Chmod(path, info.Mode().Perm()&^0o022); chmodErr != nil {
			return fmt.Errorf("chmod %s: %w", path, chmodErr)
		}

		return nil
	})
	if err != nil {
		m.t.Fatalf("drop group write: %v", err)
	}
}

func countOf(calls []string, call string) int {
	n := 0

	for _, c := range calls {
		if c == call {
			n++
		}
	}

	return n
}

func (s *stagedRun) requireActivated(r harness.Result) {
	m := s.matrixCase
	m.require(r.ExitCode == 0 && !exists(s.hold) && m.rd(m.active) == m.rd(filepath.Join(m.backup, "card-candidate")) && m.runtimes(stNew),
		"activates exactly the staged pair", r)

	for _, j := range []struct{ unit, job string }{{m.issuerUnit, "17"}, {m.collectorUnit, "18"}} {
		fields := strings.Fields(m.rd(filepath.Join(m.backup, "resume-"+j.unit+".start")))
		m.require(len(fields) >= 5 && slices.Equal(fields[1:3], []string{j.job, strings.Repeat("0", 31) + "2"}) && fields[4] == strings.Repeat("b", 64),
			"retains original "+j.unit+" job, invocation and generation", r)
	}

	checked := m.invoke("check", "")
	m.require(checked.ExitCode == 0, "completion checks both original launched generations", checked)
	m.wr(m.p("collector.cid"), "9")

	before := len(m.calls())

	for _, op := range []string{"check", modeResume, opRollback} {
		refused := m.invoke(op, "")
		m.require(refused.ExitCode != 0 && !mutated(m.calls()[before:]), op+" cannot adopt a replaced generation or reuse a released hold", refused)
	}
}

func chmod(t interface {
	Helper()
	Fatalf(string, ...any)
}, path string, mode os.FileMode,
) {
	t.Helper()

	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func rename(t interface {
	Helper()
	Fatalf(string, ...any)
}, from, to string,
) {
	t.Helper()

	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename %s: %v", from, err)
	}
}

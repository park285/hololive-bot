package pocentral

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	scSuccess         = "success"
	scSubstitution    = "substitution_failure"
	scActivation      = "activation_failure"
	scCandidateCheck  = "candidate_check_failure"
	scDatabaseQuery   = "database_query_failure"
	scRollbackCross   = "rollback_cross"
	scForeignGuard    = "foreign_guard"
	scCollected143    = "collected_143"
	scErrorLogSeal    = "error_log_seal_failure"
	scOtherInvocation = "other_invocation"
	scErrorLog        = "error_log"
	scForeignHold     = "foreign_hold"
	opRollback        = "rollback"
	stOld             = "old"
	stNew             = "new"
	stTrue            = "true"
	stFalse           = "false"
	heldStart         = "systemctl --show-transaction start "
	guardedHold       = "guarded-stop-v1\n"
	unconfirmed       = "[OUTCOME] unconfirmed:"
	otherPooler       = "other-pooler"
)

var mutations = []string{
	"podman tag ", "podman load ", "podman image rm ", "systemctl restart ", heldStart, "systemctl stop ", "mask link ",
	"guard arm ", "guard seal ", "guard undo-unmask ", "systemctl unmask ", "systemctl daemon-reload", "pair activate ",
}

type centralExpect struct {
	status   int
	restored bool
}

var centralCases = []struct {
	centralExpect

	name string
}{
	{centralExpect{0, false}, scSuccess},
	{centralExpect{0, false}, "absent_success"},
	{centralExpect{1, true}, scSubstitution},
	{centralExpect{5, true}, "deploy_logs_collector"},
	{centralExpect{1, true}, "deploy_error_log"},
	{centralExpect{2, true}, "deploy_grep_failure"},
	{centralExpect{143, true}, "signal_TERM"},
	{centralExpect{129, true}, "signal_HUP"},
	{centralExpect{137, false}, "client_lost_pending"},
	{centralExpect{1, false}, "manager_probe_failure"},
	{centralExpect{1, false}, "restore_failure"},
	{centralExpect{7, false}, scActivation},
	{centralExpect{7, false}, scCandidateCheck},
	{centralExpect{7, false}, "absent_candidate_check_failure"},
	{centralExpect{1, false}, "missing_receipt"},
	{centralExpect{1, false}, "collector_stopped"},
	{centralExpect{1, false}, "inventory_failure"},
	{centralExpect{0, true}, opRollback},
	{centralExpect{0, false}, "check_success"},
	{centralExpect{1, false}, "check_error_log"},
	{centralExpect{0, false}, "transition_success"},
	{centralExpect{1, false}, "transition_error_log"},
	{centralExpect{1, false}, "check_wrong_profile"},
	{centralExpect{1, false}, scDatabaseQuery},
	{centralExpect{1, false}, "check_wrong_instance"},
	{centralExpect{1, false}, "check_waiting_handoff"},
	{centralExpect{1, false}, "rollback_masked_endpoint"},
	{centralExpect{1, false}, "rollback_missing_endpoint"},
	{centralExpect{1, false}, "transition_wrong_endpoint"},
	{centralExpect{1, false}, scRollbackCross},
	{centralExpect{1, false}, "capacity_exhausted"},
	{centralExpect{1, false}, "rollback_capacity_exhausted"},
	{centralExpect{1, false}, "unadmitted_deploy"},
	{centralExpect{1, false}, "foreign_card_pair"},
	{centralExpect{1, false}, "rollback_stale"},
}

var (
	bCentralCases = []string{
		scSuccess, opRollback, "check_success", "check_wrong_profile", scDatabaseQuery, "transition_success",
		"check_wrong_instance", "check_waiting_handoff", "unadmitted_deploy", "foreign_card_pair", "rollback_stale",
		"transition_error_log", "transition_wrong_endpoint", scRollbackCross, "rollback_masked_endpoint", "rollback_missing_endpoint",
	}
	recoveryCases = []string{
		"receipt_signal", "receipt_signal_changed", "unmask_reload", "unmask_reload_old_cid",
		"client_unknown", "foreign_named_cid", "late_notify_client", "capture_inv_changed", "capture_cid_changed", "capture_restarted",
		"error_log_replaced", "peer_probe_failure", "error_log_guard_failure", "error_log_evidence_failure",
		"signal_INT", "signal_HUP", "signal_group_TERM",
	}
	maintenanceCases = append([]string{
		scSuccess, "reboot", "mask_failure", "stop_failure", scActivation,
		"collector_143", "guard_failure", "guard_check_failure", scForeignGuard, "stop_control_pid", "blank_receipt",
		scCollected143, "died_duplicate", "died_other_cid", "died_stale", "stop_terminal_invocation", "stop_leftover_id",
		"signal_TERM", scErrorLogSeal, "rollback_issuer_failure", "arm_collector_profile",
		"other_exit", scOtherInvocation, "leftover_container", "inventory_unavailable",
		"load_collector_replaced", "load_collector_restarted", "load_issuer_invocation", "load_issuer_image",
		"load_issuer_restarted", "load_issuer_unhealthy", "load_issuer_socket", "stop_other_invocation", "stop_missing_job",
		"stop_stale_job",
		"stop_peer_restarted", "mask_race", "mask_replaced",
		"missing_stop_receipt", scDatabaseQuery, "rollback_image", "rollback_profile", "rollback_unhealthy",
		scCandidateCheck, scSubstitution, scErrorLog, "unmask_failure",
		"source_changed", "identity_changed", "inputs_changed", scForeignHold, "lock_busy",
		"operator_mask", "incomplete_hold", "candidate_unit_changed", "candidate_profile_changed", "wrong_endpoint",
		"installed_unit_changed", "helper_changed", "migration_changed", "source_manifest_changed",
		"candidate_image_overlay_changed", "candidate_endpoint_overlay_changed",
		scRollbackCross, "rollback_coordinated", "capacity_exhausted",
		"previous_candidate", "origin_receipt_changed", "copied_hold_receipt", "lost_seal", "seal_changed",
		"rollback_replaced",
	}, recoveryCases...)
	stagedScenarios = []string{
		scSuccess, "already_loaded", scErrorLog, "missing_start_record", "archive_drift", "partial_transport", "origin_stale", "unsafe_stage",
		"changed_seal", "absent_hold", scForeignHold, "missing_seal", "other_owner", scOtherInvocation, "other_cid", "old_start_cid",
		scCandidateCheck, scDatabaseQuery, "profile_drift", "wrong_window", "wrong_boot", "wrong_tool",
		"expire_after_stop", "expire_after_unmask", "expire_after_issuer", "expired_late_notify_client", "running_other_endpoint_origin",
	}
)

func matrixRuns() [][2]string {
	var runs [][2]string

	for _, c := range centralCases {
		runs = append(runs, [2]string{"c", c.name})
	}

	for _, name := range bCentralCases {
		runs = append(runs, [2]string{"b", name})
	}

	for _, slot := range []string{"c", "b"} {
		for _, name := range maintenanceCases {
			if (slot == "c" || !strings.HasPrefix(name, "capacity_")) && (slot == "b" || name != "installed_unit_changed") {
				runs = append(runs, [2]string{slot, "maintenance_" + name})
			}
		}
	}

	for _, name := range stagedScenarios {
		runs = append(runs, [2]string{"b", "staged_" + name})
	}

	return runs
}

func expectFor(name string) centralExpect {
	for _, c := range centralCases {
		if c.name == name {
			return c.centralExpect
		}
	}

	return centralExpect{}
}

type matrixEnv struct {
	setup, functions, config, main, sourceGate string
	preambles                                  map[string]string
	slots                                      map[string]map[string]string
}

func newMatrixEnv(t *testing.T) *matrixEnv {
	t.Helper()

	s := load(t)

	var functions []string

	for _, m := range regexp.MustCompile(`(?ms)^([a-z_]+)\(\) \{\n.*?\n\}`).FindAllStringSubmatch(s.central, -1) {
		if m[1] != "require_collector_lease_migration" && m[1] != "restore_after_failed_cutover" {
			functions = append(functions, m[0])
		}
	}

	return &matrixEnv{
		setup:      centralSetup + between(t, s.central, "[[ $#", `[[ "${slot_slot:-}"`),
		functions:  strings.Join(functions, "\n"),
		config:     between(t, s.central, `manifest="../`, `[[ "$(stat`),
		main:       between(t, s.central, "{\nclaim_slot\n", ""),
		sourceGate: between(t, s.central, "cd \"$current\"\n", "compose_env="),
		preambles:  map[string]string{"c": slotPreamble(t, "c"), "b": slotPreamble(t, "b")},
		slots:      s.slots,
	}
}

func TestCentralCutoverMatrix(t *testing.T) {
	t.Parallel()

	env := newMatrixEnv(t)

	for _, r := range matrixRuns() {
		t.Run(r[0]+"/"+r[1], func(t *testing.T) {
			t.Parallel()
			newMatrixCase(t, env, r[0], r[1]).run()
		})
	}
}

type matrixCase struct {
	t                                                    *testing.T
	env                                                  *matrixEnv
	slot, name, label, service                           string
	collectorUnit, issuerUnit, bUnitSource               string
	state, current, staging, backup, priorBackup, active string
	livePO, previousPair                                 string
	host                                                 string
	dbPort                                               int
	installed, previous, originPair                      string
	stagedPair, isCheck, isRollback, issuerAbsent        bool
	manager, body                                        string
	expect                                               centralExpect
}

func (m *matrixCase) p(name string) string { return filepath.Join(m.state, name) }

func (m *matrixCase) wr(path, content string) {
	m.t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- Python fixture와 같은 기본 umask 권한을 재현한다.
		m.t.Fatalf("mkdir %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { // #nosec G306 -- Python fixture와 같은 기본 umask 권한을 재현한다.
		m.t.Fatalf("write %s: %v", path, err)
	}
}

func (m *matrixCase) rd(path string) string {
	m.t.Helper()

	return harness.Read(m.t, path)
}

func (m *matrixCase) mkdir(path string) {
	m.t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil { // #nosec G301 -- Python fixture와 같은 기본 umask 권한을 재현한다.
		m.t.Fatalf("mkdir %s: %v", path, err)
	}
}

func (m *matrixCase) touch(path string) { m.wr(path, "") }

func (m *matrixCase) imageFile(ref string) string {
	return filepath.Join(m.state, "images", strings.NewReplacer("/", "_", ":", "_").Replace(ref))
}

func (m *matrixCase) calls() []string { return lines(m.t, m.p("calls")) }

func (m *matrixCase) invoke(mode, failure string) harness.Result {
	m.t.Helper()

	fixtureCase := "maintenance_" + mode

	if failure != "" {
		fixtureCase += "_" + failure
	}

	return run(m.t, m.body, m.state, fixtureCase)
}

func (m *matrixCase) require(ok bool, message string, r harness.Result) {
	m.t.Helper()
	require(m.t, ok, m.label+": "+message, r)
}

func (m *matrixCase) runtimes(want string) bool {
	return m.rd(m.p("issuer.runtime")) == want && m.rd(m.p("collector.runtime")) == want
}

func (m *matrixCase) isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeSymlink != 0
}

func (m *matrixCase) isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}

func startsAny(call string, prefixes ...string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(call, p) })
}

func mutated(calls []string) bool {
	return slices.ContainsFunc(calls, func(c string) bool { return startsAny(c, mutations...) })
}

func withPrefix(calls []string, prefixes ...string) []string {
	out := make([]string, 0, len(calls))

	for _, c := range calls {
		if startsAny(c, prefixes...) {
			out = append(out, c)
		}
	}

	return out
}

func prefixed(prefix string, units ...string) []string {
	out := make([]string, 0, len(units))
	for _, u := range units {
		out = append(out, prefix+u)
	}

	return out
}

func activatesPrevious(calls []string) bool {
	return slices.ContainsFunc(calls, func(c string) bool {
		return strings.HasPrefix(c, "pair activate ") && strings.HasSuffix(c, "/previous")
	})
}

func manifestJSON(revision, identity string) string {
	return `{"schema_version": 1, "source_revision": "` + revision + `", "go": {"goarch": "arm64"}, "architecture": "arm64", "image_id": "` + identity + `"}`
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

func (m *matrixCase) dirPair(dir, revision, id string) string {
	m.t.Helper()

	var st syscall.Stat_t

	if err := syscall.Stat(dir, &st); err != nil {
		m.t.Fatalf("stat %s: %v", dir, err)
	}

	return fmt.Sprintf("%s %d:%d %s %s %s\n", dir, st.Dev, st.Ino, revision, id, id)
}

func (m *matrixCase) sumsOf(dir string, names ...string) string {
	var b strings.Builder

	for _, n := range names {
		b.WriteString(sha256Hex(m.rd(filepath.Join(dir, n))) + "  " + n + "\n")
	}

	return b.String()
}

func container(slots map[string]map[string]string, slot string) string {
	return strings.TrimSuffix(slots[slot]["collector_unit"], ".service")
}

func newMatrixCase(t *testing.T, env *matrixEnv, slot, name string) *matrixCase {
	t.Helper()

	state := t.TempDir()
	if err := os.Chmod(state, 0o700); err != nil { // #nosec G302 -- Python TemporaryDirectory와 같은 0700 디렉터리다.
		t.Fatalf("chmod state: %v", err)
	}

	row := env.slots[slot]
	m := &matrixCase{
		t: t, env: env, slot: slot, name: name, label: slot + " " + name, service: row["service"],
		collectorUnit: row["collector_unit"], issuerUnit: row["issuer_unit"],
		bUnitSource: env.slots["b"]["unit_dir"] + "/" + container(env.slots, "b") + ".container",
		state:       state, current: filepath.Join(state, "current"), staging: filepath.Join(state, "staging"),
		backup: filepath.Join(state, "card-po-"+slot+"-"+change), expect: expectFor(name),
	}
	m.setup()

	return m
}

func (m *matrixCase) setup() {
	m.stagedPair = strings.HasPrefix(m.name, "staged_")
	if m.stagedPair {
		m.backup = m.p("staged-po-b-" + change)
		m.touch(m.p("staged-pair"))
	}

	for _, dir := range []string{m.current, m.staging, m.p("images"), m.p("units"), m.p("po-current-" + m.slot)} {
		m.mkdir(dir)
	}

	m.host, m.dbPort = primaryHost, 5432
	if m.slot == "b" {
		m.host, m.dbPort = "100.100.1.8", 5433
	}

	port := strconv.Itoa(m.dbPort)

	m.installed = "[Container]\nEnvironment=POSTGRES_HOST=" + m.host + "\nEnvironment=POSTGRES_PORT=" + port + "\n"

	if m.slot == "c" {
		m.installed += "Environment=POSTGRES_POOL_MAX_CONNS=${COLLECTOR_POSTGRES_POOL_MAX_CONNS}\n[Service]\nEnvironment=COLLECTOR_POSTGRES_POOL_MAX_CONNS=6\nEnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env\n"
	} else {
		m.installed = strings.ReplaceAll(m.rd(harness.Path(m.t, m.bUnitSource)), "Environment=POSTGRES_PORT=5433", "Environment=POSTGRES_PORT="+port)
	}

	previousHost, previousPort := m.host, port
	if m.name == scRollbackCross {
		previousHost, previousPort = m.foreignEndpoint()
	}

	m.previous = strings.ReplaceAll(strings.ReplaceAll(m.installed, "POSTGRES_HOST="+m.host, "POSTGRES_HOST="+previousHost),
		"POSTGRES_PORT="+port, "POSTGRES_PORT="+previousPort) + "# previous revision\n"
	m.wr(m.p("units/"+container(m.env.slots, m.slot)+".container"), m.installed)
	m.wr(m.p("compose.env"), "APP_ENV=production\n")

	m.setupSources()

	if m.name == "capacity_exhausted" || m.name == "rollback_capacity_exhausted" {
		m.wr(m.p("compose.env"), "COLLECTOR_POSTGRES_POOL_MAX_CONNS=8\n")
	}

	m.isCheck, m.isRollback = strings.HasPrefix(m.name, "check_"), strings.HasPrefix(m.name, opRollback)
	m.issuerAbsent = m.name == "absent_success" || m.name == "absent_candidate_check_failure"
	m.setupRuntime()
	m.setupOrigin()

	if m.isCheck || m.isRollback {
		m.setupCandidateBackup()
	}

	m.manager = m.env.preambles[m.slot] + "slot=" + m.slot + " service=\"$slot_service\" port=\"$slot_port\" host_label=\"$slot_host\"\n" +
		m.env.setup + m.env.config + "\nsystem_unit_dir=\"$fixture_root/system-units\"\n"
	m.body = m.manager + m.env.functions + "\n" + m.env.sourceGate + "\n" + m.env.main
	m.mkdir(m.p("system-units"))
}

func (m *matrixCase) setupSources() {
	for _, unit := range []string{"hololive-api", "hololive-alarm-worker"} {
		m.wr(m.p("units/"+unit+".container"), m.rd(harness.Path(m.t, "deploy/quadlet/hololive-bot/"+unit+".container")))
	}

	sources := strings.Split(strings.TrimRight(m.rd(harness.Path(m.t, "scripts/deploy/po-central-files.txt")), "\n"), "\n")
	for _, name := range sources {
		m.wr(filepath.Join(m.staging, name), m.rd(harness.Path(m.t, name)))
	}

	m.wr(filepath.Join(m.staging, "source-files.sha256"), m.sumsOf(m.staging, sources...))
}

func (m *matrixCase) foreignEndpoint() (string, string) {
	if m.slot == "c" {
		return otherPooler, "6000"
	}

	return "100.100.1.8", "6001"
}

func (m *matrixCase) initial() string {
	if m.isCheck || m.isRollback {
		return stNew
	}

	return stOld
}

func (m *matrixCase) setupRuntime() {
	initial := m.initial()
	m.wr(m.imageFile(collectorImage(m)+":prod"), initial)
	m.wr(m.imageFile(oldID), stOld)

	if !m.issuerAbsent {
		m.wr(m.imageFile(issuerImage(m)+":prod"), initial)
	}

	cid := 1

	if m.stagedPair {
		cid = 3
	}

	m.wr(m.p("collector.generation"), `{"container_id":"`+fmt.Sprintf("%064d", cid)+`","pid":42}`+"\n")
	m.wr(m.p("issuer.started"), "1")
	m.wr(m.p("collector.started"), "1")
	m.wr(m.p("loaded.profile"), "retained profile\n")

	for _, kind := range []string{"collector", "issuer"} {
		absent := kind == "issuer" && m.issuerAbsent
		m.wr(m.p(kind+".invocation"), fmt.Sprintf("%032d\n", 1))

		runtime, running := initial, stTrue

		if absent {
			runtime = "absent"
		}

		if absent || (kind == "collector" && m.name == "collector_stopped") {
			running = stFalse
		}

		m.wr(m.p(kind+".runtime"), runtime)
		m.wr(m.p(kind+".running"), running)
	}

	dropin := "initial"

	if m.isCheck || m.isRollback {
		dropin = "candidate"
	}

	m.wr(m.p("dropin"), dropin)

	for _, p := range [][2]string{{"", rootfsManifest}, {"collector-", collectorManifest}} {
		tar := filepath.Join(m.staging, p[0]+"image.tar")
		m.wr(tar, "reviewed fixture image")
		m.wr(filepath.Join(m.staging, p[0]+"image.tar.sha256"), sha256Hex(m.rd(tar))+"  "+filepath.Base(tar)+"\n")
		m.wr(filepath.Join(m.staging, p[0]+"image-id"), newID)
		m.wr(filepath.Join(m.staging, p[1]), manifestJSON(newRevision, newID))
	}
}

func collectorImage(m *matrixCase) string { return m.env.slots["c"]["collector_image"] }

func issuerImage(m *matrixCase) string { return m.env.slots["c"]["issuer_image"] }

func (m *matrixCase) setupOrigin() {
	m.active = m.p("po-" + m.slot + ".card-active")
	m.priorBackup = filepath.Join(filepath.Dir(m.backup), "card-po-"+m.slot+"-20261002T000000Z")
	m.mkdir(m.priorBackup)

	m.originPair = m.dirPair(m.priorBackup, oldRevision, oldID)

	if m.stagedPair {
		m.wr(m.active, m.originPair)
		m.wr(filepath.Join(m.priorBackup, "card-candidate"), m.originPair)

		generation := ""

		for _, g := range []struct {
			kind string
			cid  int
		}{{"collector", 3}, {"issuer", 4}} {
			generation += fmt.Sprintf("%s %032d %064d %s 1 43 42\n", g.kind, 1, g.cid, strings.Repeat("a", 64))
		}

		m.wr(m.p("approved-origin"), fmt.Sprintf("%s %s %s %s %s %d %040d\n", strings.Repeat("1", 32), oldRevision, oldID, oldID,
			sha256Hex(m.originPair+generation), nowUnix()+3600, 1))
	}

	switch {
	case m.stagedPair || strings.HasPrefix(m.name, "maintenance_") && !strings.HasPrefix(m.name, "maintenance_rollback_"):
		if m.name == "maintenance_previous_candidate" {
			m.wr(filepath.Join(m.staging, "collector-image-id"), oldID)
		}

		transport := filepath.Join(m.staging, "transport-files.sha256")
		m.wr(transport, m.sumsOf(m.staging, "source-files.sha256", rootfsManifest, collectorManifest, "image-id",
			"collector-image-id", "image.tar.sha256", "collector-image.tar.sha256"))
		m.wr(m.p("trusted-seal"), sha256Hex(m.rd(transport)))
	case !m.isCheck && !m.isRollback && m.name != "unadmitted_deploy":
		pair := m.originPair
		if m.name == "foreign_card_pair" {
			pair = strings.ReplaceAll(pair, oldID, newID)
		}

		m.wr(m.active, pair)
	}

	m.livePO = m.p("po-current-" + m.slot)

	if m.name != "missing_receipt" && !m.issuerAbsent {
		revision, id := oldRevision, oldID

		if m.initial() == stNew {
			revision, id = newRevision, newID
		}

		m.wr(filepath.Join(m.livePO, rootfsManifest), manifestJSON(revision, id))
		m.wr(filepath.Join(m.livePO, "image-id"), id)
	}
}

func (m *matrixCase) setupCandidateBackup() {
	m.wr(m.imageFile(collectorImage(m)+":rollback-"+change), stOld)
	m.wr(m.imageFile(issuerImage(m)+":rollback-"+change), stOld)
	m.mkdir(m.backup)

	candidatePair := m.dirPair(m.backup, newRevision, newID)
	m.wr(filepath.Join(m.backup, "candidate-inputs.sha256"), m.sumsOf(m.staging, "image-id", "collector-image-id", rootfsManifest,
		collectorManifest, "image.tar.sha256", "collector-image.tar.sha256", "source-files.sha256"))
	m.wr(filepath.Join(m.backup, "card-origin"), m.originPair)
	m.wr(filepath.Join(m.backup, "card-candidate"), candidatePair)

	if m.name == "rollback_stale" {
		m.wr(m.active, strings.ReplaceAll(candidatePair, m.backup, m.priorBackup))
	} else {
		m.wr(m.active, candidatePair)
	}

	m.touch(filepath.Join(m.backup, "snapshot-complete"))

	m.previousPair = filepath.Join(m.current, "artifacts/worker-profile/releases", filepath.Base(m.backup), m.service, "previous")
	m.wr(filepath.Join(m.previousPair, "profile.json"), "retained profile\n")

	if m.name != "rollback_missing_endpoint" {
		host, port := m.host, strconv.Itoa(m.dbPort)
		if m.name == scRollbackCross || m.name == "rollback_masked_endpoint" {
			host, port = m.foreignEndpoint()
		}

		m.wr(filepath.Join(m.backup, "collector.endpoint"), host+"|"+port+"\n")
	}

	if m.isCheck {
		candidate := filepath.Join(filepath.Dir(m.previousPair), "candidate")
		m.wr(filepath.Join(candidate, "profile.json"), "candidate profile\n")

		if m.name != "check_wrong_profile" {
			m.wr(m.p("loaded.profile"), "candidate profile\n")
		}
	}

	m.wr(filepath.Join(m.previousPair, "unit.container"), m.previous)

	for name, content := range map[string]string{
		"issuer.state": "present", "collector.revision": oldRevision,
		"collector-prechange-manifest.json": manifestJSON(oldRevision, oldID), "collector-prechange.image-id": oldID,
		"po-prechange-manifest.json": manifestJSON(oldRevision, oldID), "po-prechange.image-id": oldID,
		"change-started-at": "2026-10-03T00:00:00Z",
	} {
		m.wr(filepath.Join(m.backup, name), content)
	}
}

func (m *matrixCase) run() {
	switch {
	case m.stagedPair:
		m.runStaged()
	case strings.HasPrefix(m.name, "maintenance_"):
		m.runMaintenance()
	default:
		m.runCentral()
	}
}

func nowUnix() int64 { return time.Now().Unix() }

func sourceRevision(t *testing.T, path string) string {
	t.Helper()

	var receipt struct {
		SourceRevision string `json:"source_revision"`
	}

	if err := jsonv2.Unmarshal([]byte(harness.Read(t, path)), &receipt); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	return receipt.SourceRevision
}

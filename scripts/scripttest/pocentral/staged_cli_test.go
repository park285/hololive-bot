package pocentral

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	toolRevision = "0000000000000000000000000000000000000001"
	ownerWindow  = "11111111111111111111111111111111"
	originDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	versionName  = "hololive/hololive-api/VERSION"
)

var (
	w2Issuer      = "sha256:" + strings.Repeat("9", 64)
	transportList = []string{
		"source-files.sha256", rootfsManifest, collectorManifest, "image-id", "collector-image-id",
		"image.tar.sha256", "collector-image.tar.sha256",
	}
	migrationSources = []string{"hololive/hololive-api/scripts/migrations/002_epoch4_fixture.sql", "hololive/hololive-api/scripts/migrations/contracts/002_epoch4_fixture.sql"}
	coordinatorFiles = []string{
		"scripts/deploy/po-central-cutover.sh", "scripts/deploy/lib/source-revision.sh", "scripts/deploy/lib/po-central-remote.sh",
		"scripts/deploy/lib/collector-slots.sh", "scripts/deploy/lib/collector-slots.tsv", "scripts/deploy/lib/migration-receipts.sh",
	}
)

func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()

	for _, rel := range filesUnder(t, from) {
		copyFile(t, filepath.Join(from, rel), filepath.Join(to, rel))
	}
}

type stagedCLI struct {
	t                                            *testing.T
	state, tool, candidate, archive, versionFile string
	seal                                         string
	expiry                                       int64
	consumed                                     int64
}

type grantSpec struct {
	mode, window, issuer, tool, stage string
	expires                           int64
}

func newStagedCLI(t *testing.T) *stagedCLI {
	t.Helper()

	state := t.TempDir()
	if err := os.Chmod(state, 0o700); err != nil { // #nosec G302 -- 영수증 디렉터리는 실행 사용자 전용 0700이어야 한다.
		t.Fatalf("chmod receipt directory: %v", err)
	}

	s := &stagedCLI{
		t: t, state: state, tool: filepath.Join(state, "coordinator"), candidate: filepath.Join(state, "candidate"),
		archive: filepath.Join(state, "arm64"), expiry: time.Now().Unix() + 7200,
	}

	s.versionFile = filepath.Join(s.candidate, versionName)

	for _, name := range coordinatorFiles {
		copyFile(t, harness.Path(t, name), filepath.Join(s.tool, name))
	}

	harness.Write(t, s.versionFile, "19.2.1\n")
	harness.Write(t, filepath.Join(s.candidate, "scripts/deploy/po-central-files.txt"), versionName+"\n")

	migrations := filepath.Join(s.candidate, "hololive/hololive-api/scripts/migrations")
	harness.Write(t, filepath.Join(migrations, "manifest.txt"), "1 001_schema_epoch4_baseline.sql\n2 002_epoch4_fixture.sql\n")

	for _, name := range []string{"001_schema_epoch4_baseline.sql", "002_epoch4_fixture.sql", "contracts/002_epoch4_fixture.sql"} {
		harness.Write(t, filepath.Join(migrations, name), "SELECT '"+name+"';\n")
	}

	copyTree(t, s.tool, filepath.Join(state, "objects/coordinator"))
	copyTree(t, s.candidate, filepath.Join(state, "objects/candidate"))
	harness.Write(t, filepath.Join(s.archive, "source-files.sha256"), sums(t, s.candidate, append([]string{versionName}, migrationSources...)...))

	for _, p := range [][2]string{{"", rootfsManifest}, {"collector-", collectorManifest}} {
		name := p[0] + "image.tar"
		harness.Write(t, filepath.Join(s.archive, name), name)
		harness.Write(t, filepath.Join(s.archive, name+".sha256"), sums(t, s.archive, name))
		harness.Write(t, filepath.Join(s.archive, p[0]+"image-id"), newID+"\n")
		harness.Write(t, filepath.Join(s.archive, p[1]), `{"source_revision": "`+newRevision+`", "architecture": "arm64", "go": {"goarch": "arm64"}}`)
	}

	s.reseal()

	w2 := filepath.Join(state, "w2")
	harness.Write(t, filepath.Join(w2, "collector-image-id"), oldID+"\n")
	harness.Write(t, filepath.Join(w2, "image-id"), w2Issuer+"\n")
	harness.Write(t, filepath.Join(w2, collectorManifest), `{"source_revision": "`+oldRevision+`", "go": {"goarch": "arm64"}}`)
	harness.Write(t, filepath.Join(w2, rootfsManifest), `{"source_revision": "`+oldRevision+`", "architecture": "arm64"}`)
	harness.Write(t, s.path("b.window-clear"), "IRIS_WINDOW_CLEAR "+newRevision+"\n")
	harness.Write(t, s.path("c.accepted"), newRevision+" "+s.seal+"\n")
	s.stageReceipt(change, s.seal)

	return s
}

func (s *stagedCLI) path(name string) string { return filepath.Join(s.state, name) }

func (s *stagedCLI) reseal() {
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), sums(s.t, s.archive, transportList...))

	s.seal = digest(s.t, filepath.Join(s.archive, "transport-files.sha256"))
}

func (s *stagedCLI) stageReceipt(stage, seal string) {
	harness.Write(s.t, s.path("b.stage."+stage+".finished"), newRevision+" "+seal+" "+stage+"\n")
}

func (s *stagedCLI) grant(g grantSpec) {
	s.t.Helper()

	g.window = cmpOr(g.window, ownerWindow)
	g.issuer = cmpOr(g.issuer, w2Issuer)
	g.tool = cmpOr(g.tool, toolRevision)
	g.stage = cmpOr(g.stage, change)

	if g.expires == 0 {
		g.expires = s.expiry
	}

	line := func(mode string) string {
		return strings.Join([]string{
			"PO_B_PAIR_V1 iris-seoul", g.window, mode, g.stage, g.tool, newRevision, s.seal,
			oldRevision, oldID, g.issuer, originDigest, strconv.FormatInt(g.expires, 10),
		}, " ") + "\n"
	}

	harness.Write(s.t, s.path("grant.tmp"), line(g.mode))

	if err := os.Rename(s.path("grant.tmp"), s.path("b.current-grant")); err != nil {
		s.t.Fatalf("publish grant: %v", err)
	}

	harness.Write(s.t, s.path("prepared-resume-grant"), line(modeResume))
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func (s *stagedCLI) run(prefix, mode string) harness.Result {
	s.t.Helper()

	return run(s.t, prefix+stagedEntry, s.state, mode)
}

func (s *stagedCLI) untouched(r harness.Result, message string) {
	s.t.Helper()
	require(s.t, r.ExitCode != 0 && !exists(s.path("calls")) && exists(s.path("b.current-grant")) &&
		!exists(s.path("b.quiesce."+change+".requested")), message, r)
}

func (s *stagedCLI) noSSH(r harness.Result, message string) {
	s.t.Helper()
	require(s.t, r.ExitCode != 0 && !exists(s.path("calls")), message, r)
}

func (s *stagedCLI) nlink(path string) uint64 {
	s.t.Helper()

	var st syscall.Stat_t

	if err := syscall.Lstat(path, &st); err != nil {
		s.t.Fatalf("stat %s: %v", path, err)
	}

	return st.Nlink
}

func (s *stagedCLI) bothModes(prefix, message string) {
	s.t.Helper()

	for _, mode := range []string{modePreflight, modeQuiesce} {
		s.untouched(s.run(prefix, mode), mode+": "+message)
	}
}

func (s *stagedCLI) refusesWithoutAuthority() {
	for _, c := range []struct {
		reason string
		grant  *grantSpec
	}{
		{"no-grant", nil},
		{"old-window", &grantSpec{mode: modeQuiesce, window: strings.Repeat("3", 32)}},
		{"expired", &grantSpec{mode: modeQuiesce, expires: 1}},
		{"wrong-phase", &grantSpec{mode: modeResume}},
		{"wrong-tool", &grantSpec{mode: modeQuiesce, tool: strings.Repeat("e", 40)}},
		{"wrong-W2-payload", &grantSpec{mode: modeQuiesce, issuer: oldID}},
	} {
		if c.grant != nil {
			s.grant(*c.grant)
		}

		s.noSSH(s.run("", modeQuiesce), c.reason+" has no SSH authority")
	}

	s.grant(grantSpec{mode: modeQuiesce})

	for _, c := range [][2]string{{s.tool, "tool_root"}, {s.candidate, "product_root"}} {
		nested := filepath.Join(c[0], "tmp/ignored")
		copyTree(s.t, filepath.Join(s.state, "objects", filepath.Base(c[0])), nested)
		s.noSSH(s.run(c[1]+"="+shq(nested)+"\n", modeQuiesce), "ignored nested "+c[1]+" is not the checkout")
	}

	for _, path := range []string{
		filepath.Join(s.tool, "scripts/deploy/po-central-cutover.sh"), filepath.Join(s.tool, "scripts/deploy/lib/source-revision.sh"),
		s.versionFile, filepath.Join(s.candidate, "scripts/deploy/po-central-files.txt"),
	} {
		original := harness.Read(s.t, path)
		harness.Write(s.t, path, original+"\n")
		s.noSSH(s.run("", modeQuiesce), "diverging Git blob rejected: "+filepath.Base(path))
		harness.Write(s.t, path, original)
	}

	s.stageReceipt(change, strings.Repeat("d", 64))
	s.noSSH(s.run("", modeQuiesce), "foreign archive seal refused locally")
	s.stageReceipt(change, s.seal)
	harness.Write(s.t, filepath.Join(s.archive, "image.tar"), "unaccepted archive")
	s.noSSH(s.run("", modeQuiesce), "locally drifted archive refused before SSH")
	harness.Write(s.t, filepath.Join(s.archive, "image.tar"), "image.tar")
}

func (s *stagedCLI) refusesRemoteRejectedChecksumLine() {
	issuerSum := filepath.Join(s.archive, "image.tar.sha256")
	goodIssuerSum, goodTransport, goodSeal := harness.Read(s.t, issuerSum), harness.Read(s.t, filepath.Join(s.archive, "transport-files.sha256")), s.seal
	harness.Write(s.t, issuerSum, strings.ReplaceAll(goodIssuerSum, "  image.tar", "  ./image.tar"))
	s.reseal()
	s.stageReceipt(change, s.seal)
	s.grant(grantSpec{mode: modeQuiesce})

	for _, mode := range []string{modePreflight, modeQuiesce} {
		prefix := ""

		if mode == modePreflight {
			prefix = "deploy_approval=false\n"
		}

		r := s.run(prefix, mode)
		require(s.t, r.ExitCode != 0 && !exists(s.path("calls")) && exists(s.path("b.current-grant")),
			mode+" refuses a checksum line the remote rejects before consuming the grant", r)
	}

	harness.Write(s.t, issuerSum, goodIssuerSum)
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), goodTransport)

	s.seal = goodSeal
	s.stageReceipt(change, s.seal)
}

func (s *stagedCLI) names() []string {
	s.t.Helper()

	entries, err := os.ReadDir(s.state)
	if err != nil {
		s.t.Fatalf("read state: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func (s *stagedCLI) preflightAndGrantLock() {
	s.grant(grantSpec{mode: modeQuiesce})

	before := s.names()
	checked := s.run("deploy_approval=false\n", modePreflight)
	require(s.t, checked.ExitCode == 0 && !exists(s.path("calls")) && slices.Equal(s.names(), before) && s.nlink(s.path("b.current-grant")) == 1,
		"preflight runs the quiesce checks without SSH or grant consumption", checked)
	s.noSSH(s.run("receipt_arg="+shq(s.state)+"/\ndeploy_approval=false\n", modePreflight), "preflight refuses the receipt path that quiesce refuses")

	held, err := os.OpenFile(s.path("receipts.lock"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		s.t.Fatalf("open receipts.lock: %v", err)
	}

	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		s.t.Fatalf("flock: %v", err)
	}

	r := s.run("", modeQuiesce)

	if err := held.Close(); err != nil {
		s.t.Fatalf("close receipts.lock: %v", err)
	}

	s.untouched(r, "grant consumption refuses a concurrent publication")

	replaced := s.run(`flock() { python3 -c "import sys; p = sys.argv[1]; open(p + '.new', 'w').write(open(p).read().replace(' quiesce ', ' quiesce ').rsplit(' ', 1)[0] + ' 9999999999\n')" "$state/b.current-grant" && chmod 600 "$state/b.current-grant.new" && mv -f "$state/b.current-grant.new" "$state/b.current-grant"; }
export -f flock
`, modeQuiesce)
	require(s.t, replaced.ExitCode != 0 && !exists(s.path("calls")) && s.nlink(s.path("b.current-grant")) == 1 &&
		!exists(s.path("b.quiesce."+change+".requested")) && !exists(s.path("b."+ownerWindow+".quiesce.used")),
		"a grant replaced before the lock is revalidated and left unconsumed", replaced)
}

func (s *stagedCLI) refusesUnstableInputs() {
	s.grant(grantSpec{mode: modeQuiesce})

	tarball, elsewhere := filepath.Join(s.archive, "image.tar"), filepath.Join(s.archive, "elsewhere/image.tar")
	copyFile(s.t, tarball, elsewhere)
	harness.Remove(s.t, tarball)
	harness.Symlink(s.t, elsewhere, tarball)

	for _, mode := range []string{modePreflight, modeQuiesce} {
		r := s.run("", mode)
		require(s.t, r.ExitCode != 0 && !exists(s.path("calls")) && exists(s.path("b.current-grant")), mode+" refuses a symlinked archive that resume would rehash", r)
	}

	harness.Remove(s.t, tarball)

	if err := os.Rename(elsewhere, tarball); err != nil {
		s.t.Fatalf("restore archive: %v", err)
	}

	harness.Remove(s.t, filepath.Dir(elsewhere))

	s.grant(grantSpec{mode: modeQuiesce, expires: time.Now().Unix() + 300})
	s.bothModes("", "refuses a quiesce grant that expires before b resume")

	s.grant(grantSpec{mode: modeQuiesce})

	earlier := harness.Read(s.t, s.path("prepared-resume-grant"))
	s.grant(grantSpec{mode: modeQuiesce, expires: s.expiry + 60})
	harness.Write(s.t, s.path("prepared-resume-grant"), earlier)
	s.bothModes("", "refuses a stale prepared resume grant before anything stops")
	s.bothModes("resume_grant_arg="+shq(s.path("absent-resume-grant"))+"\n", "refuses a missing prepared resume grant before anything stops")

	s.grant(grantSpec{mode: modeQuiesce})
	s.refusesOneShotResumeGrant()

	s.grant(grantSpec{mode: modeQuiesce, expires: time.Now().Unix() + 4500})
	s.noSSH(s.run("deploy_approval=false\n", modePreflight), "preflight keeps time to stop d before b quiesce consumes the grant")

	s.grant(grantSpec{mode: modeQuiesce, expires: time.Now().Unix() + 3603})

	r := s.run("sync() { sleep 4; command sync \"$@\"; }\nexport -f sync\n", modeQuiesce)
	s.untouched(r, "quiesce rechecks the one-hour floor after a slow sync and before consuming the grant")
	require(s.t, !exists(s.path("b."+ownerWindow+".quiesce.used")), "a refused quiesce leaves the window unused", r)
}

func (s *stagedCLI) refusesOneShotResumeGrant() {
	fifo := s.path("resume-grant.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		s.t.Fatalf("mkfifo: %v", err)
	}

	grant := harness.Read(s.t, s.path("prepared-resume-grant"))

	for _, mode := range []string{modePreflight, modeQuiesce} {
		fed := make(chan error, 1)

		go func() {
			pipe, err := os.OpenFile(fifo, os.O_WRONLY, 0) // #nosec G304 -- 테스트가 만든 FIFO 경로다.
			if err != nil {
				fed <- err

				return
			}

			_, err = pipe.WriteString(grant)
			fed <- errors.Join(err, pipe.Close())
		}()

		r := s.run("resume_grant_arg="+shq(fifo)+"\n", mode)

		drain, err := syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			s.t.Fatalf("drain fifo: %v", err)
		}

		select {
		case err := <-fed:
			if err != nil && !errors.Is(err, syscall.EPIPE) {
				s.t.Fatalf("feed fifo: %v", err)
			}
		case <-time.After(5 * time.Second):
		}

		if err := syscall.Close(drain); err != nil {
			s.t.Fatalf("close fifo: %v", err)
		}

		s.untouched(r, mode+" refuses a one-shot resume grant that step 5 could not read again")
	}

	harness.Remove(s.t, fifo)
}

func (s *stagedCLI) refusesSwappedResumeGrant() {
	s.grant(grantSpec{mode: modeQuiesce})

	swapped := s.path("swap-resume-grant")
	copyFile(s.t, s.path("prepared-resume-grant"), swapped)

	for _, c := range [][2]string{{`exec 7<>"$state/swap-resume-grant"; printf "%s\n" "$line" >&7; `, "holding the right line"}, {"", "without a writer"}} {
		swap := `swap_resume_fifo() { if [[ "$*" == *swap-resume-grant* ]]; then local line; line="$(cat -- "$state/swap-resume-grant")"; ` +
			`rm -f -- "$state/swap-resume-grant"; mkfifo -- "$state/swap-resume-grant"; ` + c[0] + "fi; }\n" +
			"python3() { swap_resume_fifo \"$@\"; command python3 \"$@\"; }\n" +
			"export -f swap_resume_fifo python3\nresume_grant_arg=" + shq(swapped) + "\n"

		for _, mode := range []string{modePreflight, modeQuiesce} {
			r := s.run(swap, mode)
			s.untouched(r, mode+" refuses a resume grant path that turns into a FIFO "+c[1])
			require(s.t, strings.Contains(r.Stderr, "prepared resume grant must be a regular file"), "refused for the FIFO itself", r)
			harness.Remove(s.t, swapped)
			copyFile(s.t, s.path("prepared-resume-grant"), swapped)
		}
	}

	argvLog := s.path("python-argv")
	accepted := s.run("python3() { printf \"%s\\n\" \"$*\" >> "+shq(argvLog)+"; command python3 \"$@\"; }\nexport -f python3\ndeploy_approval=false\n", modePreflight)
	require(s.t, accepted.ExitCode == 0 && exists(argvLog) &&
		!strings.Contains(harness.Read(s.t, argvLog), strings.TrimSpace(harness.Read(s.t, s.path("prepared-resume-grant")))),
		"preflight compares the resume grant without passing its bytes as process arguments", accepted)
	harness.Remove(s.t, argvLog)
	harness.Remove(s.t, swapped)
}

func (s *stagedCLI) refusesMovedArchiveAndUsedWindow() {
	moved := s.path("arm64-real")
	if err := os.Rename(s.archive, moved); err != nil {
		s.t.Fatalf("move archive: %v", err)
	}

	harness.Symlink(s.t, moved, s.archive)

	for _, mode := range []string{modePreflight, modeQuiesce} {
		r := s.run("", mode)
		require(s.t, r.ExitCode != 0 && !exists(s.path("calls")) && exists(s.path("b.current-grant")), mode+" refuses a symlinked arm64 directory", r)
	}

	harness.Remove(s.t, s.archive)

	if err := os.Rename(moved, s.archive); err != nil {
		s.t.Fatalf("restore archive: %v", err)
	}

	used := s.path("b." + ownerWindow + ".quiesce.used")
	harness.Write(s.t, used, "earlier stage\n")
	s.bothModes("", "refuses a window already used by another stage without marking this stage")
	harness.Remove(s.t, used)
}

func (s *stagedCLI) lostTransportBlocksRetry() {
	lostWindow, lostChange := strings.Repeat("f", 32), "20261002T000000Z"
	lostPrefix := "window_id=" + lostWindow + "\nstage_id=" + lostChange + "\n"
	s.stageReceipt(lostChange, s.seal)
	harness.Write(s.t, s.path("lost-reply"), "")
	s.grant(grantSpec{mode: modeQuiesce, window: lostWindow, stage: lostChange})

	unknown := s.run(lostPrefix, modeQuiesce)
	require(s.t, unknown.ExitCode == 255 && exists(s.path("calls")) && !exists(s.path("b."+lostWindow+".quiesce.finished")),
		"no completed phase on lost transport", unknown)
	harness.Remove(s.t, s.path("calls"))
	harness.Remove(s.t, s.path("lost-reply"))

	for _, mode := range []string{modePreflight, modeQuiesce, modeResume} {
		s.grant(grantSpec{mode: cmpOr(map[string]string{modePreflight: modeQuiesce}[mode], mode), window: lostWindow, stage: lostChange})
		s.noSSH(s.run(lostPrefix, mode), "unknown outcome permits neither replay nor resume ("+mode+")")
	}

	for _, mode := range []string{modePreflight, modeQuiesce} {
		s.grant(grantSpec{mode: modeQuiesce, stage: lostChange})
		s.noSSH(s.run("stage_id="+lostChange+"\n", mode), "a different window cannot retry unknown stage effects ("+mode+")")
	}
}

func (s *stagedCLI) consumesEachGrantOnce() string {
	accepted := harness.Read(s.t, s.path("c.accepted"))
	s.grant(grantSpec{mode: modeResume})
	harness.Remove(s.t, s.path("c.accepted"))
	s.noSSH(s.run("", modeResume), "resume requires the accepted c pair")

	s.consumed = time.Now().Unix() + 4500

	consumed := s.consumed
	retained := s.path("b." + ownerWindow + ".resume-grant")
	publishRetained := func() { copyFile(s.t, retained, s.path("b.current-grant")) }

	var expected string

	for _, mode := range []string{modeQuiesce, modeResume} {
		if mode == modeResume {
			harness.Write(s.t, s.path("c.accepted"), accepted)
			harness.Remove(s.t, s.path("prepared-resume-grant"))
			publishRetained()
		} else {
			s.grant(grantSpec{mode: mode, expires: consumed})
		}

		r := s.run("", mode)
		remote := load(s.t).slots["b"]["root"]

		expected = strings.Join([]string{
			"iris-seoul sudo -n bash -s -- b", mode, remote + "/po-staging-" + change, remote + "/backups/staged-po-b-" + change,
			newRevision, "19.2.1", s.seal, ownerWindow, oldRevision, oldID, w2Issuer, originDigest, strconv.FormatInt(consumed, 10), toolRevision,
		}, " ")
		require(s.t, r.ExitCode == 0 && slices.Equal(lines(s.t, s.path("calls")), []string{expected}),
			"coordinator revision stays distinct; only already-staged accepted candidate addresses the writer", r)
		harness.Remove(s.t, s.path("calls"))
		require(s.t, !exists(s.path("b.current-grant")), "grant slot vacated for independent no-replace issuance", r)

		if mode == modeQuiesce {
			require(s.t, harness.Mode(s.t, retained) == 0o600 && s.nlink(retained) == 1 &&
				harness.Read(s.t, retained) == harness.Read(s.t, s.path("prepared-resume-grant")),
				"quiesce keeps the verified resume grant for step 5", r)
			s.grant(grantSpec{mode: mode, expires: consumed})
		} else {
			publishRetained()
		}

		replay := s.run("", mode)
		require(s.t, replay.ExitCode != 0 && !exists(s.path("calls")) && strings.Contains(replay.Stderr, "was already requested"),
			"the same consumed phase grant cannot be replayed", replay)
	}

	return expected
}

func lines(t *testing.T, path string) []string {
	t.Helper()

	if !exists(path) {
		return []string{}
	}

	return strings.Split(strings.TrimSuffix(harness.Read(t, path), "\n"), "\n")
}

func (s *stagedCLI) rollbackNeedsItsOwnApproval(expected string) {
	checked := s.run("deploy_approval=false\n", "check")
	require(s.t, checked.ExitCode == 0 && slices.Equal(lines(s.t, s.path("calls")), []string{strings.Replace(expected, " b resume ", " b check ", 1)}),
		"pinned completion takes no writer grant", checked)
	harness.Remove(s.t, s.path("calls"))
	s.grant(grantSpec{mode: "rollback", expires: s.consumed})
	s.noSSH(s.run("", "rollback"), "rollback requires its own approval")

	remoteTool := filepath.Join(s.tool, "scripts/deploy/lib/po-central-remote.sh")
	original := harness.Read(s.t, remoteTool)

	for _, op := range []string{"check", "rollback"} {
		harness.Write(s.t, remoteTool, original+"\n")
		s.noSSH(s.run("rollback_approval=true\n", op), op+" refuses changed remote tool")
		harness.Write(s.t, remoteTool, original)
	}

	authorized := s.run("rollback_approval=true\ndeploy_approval=false\n", "rollback")
	require(s.t, authorized.ExitCode == 0 && slices.Equal(lines(s.t, s.path("calls")), []string{strings.Replace(expected, " b resume ", " b rollback ", 1)}),
		"original tool executes separately granted rollback", authorized)
	harness.Remove(s.t, s.path("calls"))
}

func (s *stagedCLI) verifyPairJudgesBeforeAnyStop() {
	for _, name := range coordinatorFiles {
		copyFile(s.t, harness.Path(s.t, name), filepath.Join(s.candidate, name))
	}

	verify := between(s.t, stagedEntry, "", `bash "${tool_root`) + "bash \"$state/candidate/scripts/deploy/po-central-cutover.sh\" c verify-pair \"$state/arm64\" \"$2\"\n"
	judge := func(seal, message string, valid bool) {
		r := run(s.t, verify, s.state, seal)
		require(s.t, (r.ExitCode == 0) == valid && !exists(s.path("calls")), message+" is judged before any stop without SSH", r)
	}

	for _, c := range []struct{ label, damaged, seal string }{
		{"verified pair", "", s.seal},
		{"foreign seal", "", strings.Repeat("d", 64)},
		{"truncated issuer archive", "image.tar", s.seal},
		{"missing collector archive", "collector-image.tar", s.seal},
		{"symlinked issuer archive", "image.tar", s.seal},
		{"symlinked collector manifest", collectorManifest, s.seal},
	} {
		target := filepath.Join(s.archive, c.damaged)
		original := ""

		if c.damaged != "" {
			original = harness.Read(s.t, target)
		}

		switch {
		case strings.HasPrefix(c.label, "symlinked"):
			elsewhere := s.path("elsewhere/" + c.damaged)
			harness.Write(s.t, elsewhere, original)
			harness.Remove(s.t, target)
			harness.Symlink(s.t, elsewhere, target)
		case c.damaged == "image.tar":
			harness.Write(s.t, target, original[:len(original)-1])
		case c.damaged != "":
			harness.Remove(s.t, target)
		}

		judge(c.seal, c.label, c.label == "verified pair")

		if c.damaged != "" {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				s.t.Fatalf("remove %s: %v", target, err)
			}

			harness.Write(s.t, target, original)
		}
	}

	s.judgeChangedVersionAndChecksumLine(judge)
}

func (s *stagedCLI) judgeChangedVersionAndChecksumLine(judge func(seal, message string, valid bool)) {
	goodVersion := harness.Read(s.t, s.versionFile)
	goodSources, goodTransport := harness.Read(s.t, filepath.Join(s.archive, "source-files.sha256")), harness.Read(s.t, filepath.Join(s.archive, "transport-files.sha256"))
	harness.Write(s.t, s.versionFile, "19.2.1+meta\n")
	harness.Write(s.t, filepath.Join(s.archive, "source-files.sha256"), sums(s.t, s.candidate, append([]string{versionName}, migrationSources...)...))
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), sums(s.t, s.archive, transportList...))
	judge(digest(s.t, filepath.Join(s.archive, "transport-files.sha256")), "a VERSION that quiesce rejects", false)
	harness.Write(s.t, s.versionFile, goodVersion)
	harness.Write(s.t, filepath.Join(s.archive, "source-files.sha256"), goodSources)
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), goodTransport)

	issuerSum := filepath.Join(s.archive, "image.tar.sha256")
	goodIssuerSum := harness.Read(s.t, issuerSum)
	harness.Write(s.t, issuerSum, strings.ReplaceAll(goodIssuerSum, "  image.tar", "  ./image.tar"))
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), sums(s.t, s.archive, transportList...))
	judge(digest(s.t, filepath.Join(s.archive, "transport-files.sha256")), "a checksum line the remote rejects", false)
	harness.Write(s.t, issuerSum, goodIssuerSum)
	harness.Write(s.t, filepath.Join(s.archive, "transport-files.sha256"), goodTransport)
}

func TestStagedPairCLIConsumesOnlyVerifiedGrantsBeforeSSH(t *testing.T) {
	t.Parallel()

	s := newStagedCLI(t)
	s.refusesWithoutAuthority()
	s.refusesRemoteRejectedChecksumLine()
	s.preflightAndGrantLock()
	s.refusesUnstableInputs()
	s.refusesSwappedResumeGrant()
	s.refusesMovedArchiveAndUsedWindow()
	s.lostTransportBlocksRetry()
	s.rollbackNeedsItsOwnApproval(s.consumesEachGrantOnce())
	s.verifyPairJudgesBeforeAnyStop()
}

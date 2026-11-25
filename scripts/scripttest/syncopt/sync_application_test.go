package syncopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const migratorPath = "deploy/quadlet/hololive-bot/hololive-db-migrate.container"

type poolWindow struct {
	e          syncEnv
	w1, w2     string
	w1Host     syncHost
	cardHost   syncHost
	sourceRepo string
}

func protectedSnapshot(t *testing.T, h syncHost) string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{
		Script: `cd "$1"
find etc-containers-systemd usr-local-libexec etc-systemd-system etc-nftables.d etc-stack-secrets run/hololive-bot \
  opt/current/{deploy/{postgres,nginx,topology},hololive/hololive-api/scripts/init-db,data,logs,backups,runtime-config,artifacts/worker-profile} \
  -type f ! -name hololive-db-migrate.container -printf '%p %i %m %U:%G %T@ %C@\n' -exec sha256sum {} + | LC_ALL=C sort`,
		Args: []string{string(h)},
	})
	harness.Succeeds(t, "protected snapshot", r)

	return r.Stdout
}

func newPoolWindow(t *testing.T) *poolWindow {
	t.Helper()

	e := newSyncEnv(t)
	p := &poolWindow{e: e}
	base := unreviewedRuntimeRepo(t, e, "repo")

	p.w1 = e.clone(t, base, "repo-pool")

	migrator := harness.Read(t, filepath.Join(base, migratorPath))

	migrator = strings.ReplaceAll(migrator, "\nExec=--schema-only\n", "\nExec=--configure-login-roles\n")
	migrator = strings.ReplaceAll(migrator, " hololive-db-migrate schema\n", " hololive-db-migrate admin-bootstrap\n")
	overwriteText(t, filepath.Join(p.w1, migratorPath), migrator)
	commit(t, p.w1, "pool-window")

	p.w1Host = e.host(t, "host-pool", "1")
	e.syncs(t, p.w1Host, p.w1, "pool-window installation")

	for _, path := range runtimePaths {
		put(t, p.w1Host.at("opt/current/"+path+"/accepted"), "accepted runtime\n")
	}

	old := time.Unix(1, 0)
	stamped := glob(t, p.w1Host.at("opt/current/hololive/hololive-api/scripts/init-db/*"))

	for _, path := range append(stamped, p.w1Host.at("opt/current/deploy/postgres/pg_hba.conf")) {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	p.w2 = e.clone(t, base, "repo-card")
	put(t, filepath.Join(p.w2, "hololive/hololive-api/VERSION"), "card release\n")
	commit(t, p.w2, "writer-drain")

	return p
}

func TestSyncOptApplicationOnlyAndSourceOnlyPublication(t *testing.T) {
	t.Parallel()

	p := newPoolWindow(t)
	p.assertCardPublication(t)

	installed, next := p.prepareSourceOnly(t)
	p.assertSourceOnlyRejections(t, installed, next)
	p.assertUnsafeCoordinatorAuthority(t, installed)
	p.assertPartialPublication(t, installed, next)
	p.assertSourceOnlyPublication(t, installed, next)
	p.assertApplicationOnlyDrift(t)
}

func (p *poolWindow) assertCardPublication(t *testing.T) {
	t.Helper()

	h := syncHost(p.e.clone(t, string(p.w1Host), "host-card"))
	before := protectedSnapshot(t, h)
	p.e.syncs(t, h, p.w2, "writer-drain publication", "--application-only")

	if protectedSnapshot(t, h) != before {
		t.Fatal("writer drain changed a protected inode, mode or content")
	}

	if strings.Contains("\n"+h.events(t), "\nsystemctl ") {
		t.Fatal("source publication changed daemon state")
	}

	for _, pair := range [][2]string{
		{filepath.Join(p.w2, migratorPath), h.installed(filepath.Base(migratorPath))},
		{filepath.Join(p.w2, migratorPath), h.at("opt/current/" + migratorPath)},
		{filepath.Join(p.w2, "hololive/hololive-api/VERSION"), h.at("opt/current/hololive/hololive-api/VERSION")},
	} {
		if !sameFile(t, pair[0], pair[1]) {
			t.Errorf("%s was not published to %s", pair[0], pair[1])
		}
	}

	if strings.TrimSpace(harness.Read(t, h.at("opt/current/.source-revision"))) != head(t, p.w2) {
		t.Error("card deployment revision was not published")
	}

	found := backups(t, h, shortHead(t, p.w2)+"-")
	if len(found) != 1 || exists(filepath.Join(found[0], "runtime-config")) || exists(filepath.Join(found[0], "data")) {
		t.Fatalf("card rollback copy is missing or holds runtime data: %v", found)
	}

	if !sameFile(t, filepath.Join(p.w1, migratorPath), filepath.Join(found[0], migratorPath)) {
		t.Error("card rollback copy lost the previous migrator")
	}

	p.cardHost = h
}

func (p *poolWindow) prepareSourceOnly(t *testing.T) (string, string) {
	t.Helper()

	h := syncHost(p.e.clone(t, string(p.cardHost), "host-source"))

	p.sourceRepo = p.e.clone(t, p.w2, "repo-source")

	installed := head(t, p.sourceRepo)

	run(t, "", "chmod", "-R", "go-w", h.at("opt/current/scripts"), h.at("opt/current/hololive"))
	run(t, "", "chmod", "go-w", h.at("opt"), h.at("opt/current"), h.at("usr-local-libexec"))
	overwrite(t, harness.Path(t, "scripts/deploy/sync-opt-current.sh"), filepath.Join(p.sourceRepo, "scripts/deploy/sync-opt-current.sh"))
	overwriteText(t, filepath.Join(p.sourceRepo, "hololive/hololive-api/VERSION"), "new release\n")
	commit(t, p.sourceRepo, "new-release")

	p.cardHost = h

	return installed, head(t, p.sourceRepo)
}

func (p *poolWindow) assertSourceOnlyRejections(t *testing.T, installed, next string) {
	t.Helper()

	h, e := p.cardHost, p.e
	e.rejects(t, h, p.sourceRepo, "stale precursor", "installed revision mismatch", "--source-only", next)

	untracked := filepath.Join(p.sourceRepo, "untracked")
	put(t, untracked, "foreign\n")
	e.rejects(t, h, p.sourceRepo, "unreviewed source", "clean checkout", "--source-only", installed)
	harness.Remove(t, untracked)

	drift := syncHost(e.clone(t, string(h), "host-source-drift"))
	appendFile(t, drift.at("opt/current/hololive/hololive-api/VERSION"), "foreign\n")
	e.rejects(t, drift, p.sourceRepo, "unreviewed precursor bytes", "content or path mismatch", "--source-only", installed)

	deleted := e.clone(t, p.sourceRepo, "repo-source-deleted")
	harness.Remove(t, filepath.Join(deleted, "hololive/hololive-api/VERSION"))
	commit(t, deleted, "remove-source")
	e.rejects(t, h, deleted, "removed source", "regular-file modifications", "--source-only", installed)

	reserved := e.clone(t, p.sourceRepo, "repo-source-reserved")
	put(t, h.at("canary"), "retained\n")
	harness.Symlink(t, "/canary", filepath.Join(reserved, ".source-revision"))
	commit(t, reserved, "reserved-stamp")
	e.rejects(t, h, reserved, "reserved stamp", "reserved source-revision", "--source-only", installed)
	p.assertCanary(t)

	protected := e.clone(t, p.sourceRepo, "repo-source-protected")
	appendFile(t, filepath.Join(protected, "scripts/deploy/verify-exec-tree-ownership.sh"), "echo ran >/canary\n")
	commit(t, protected, "protected-change")
	e.rejects(t, h, protected, "changed install helper", "unapproved source-only path", "--source-only", installed)
	p.assertCanary(t)
}

func (p *poolWindow) assertCanary(t *testing.T) {
	t.Helper()

	if harness.Read(t, p.cardHost.at("canary")) != "retained\n" {
		t.Fatal("a rejected source-only run changed an external file")
	}
}

func (p *poolWindow) assertUnsafeCoordinatorAuthority(t *testing.T, installed string) {
	t.Helper()

	attacks := map[string]func(t *testing.T, h syncHost, entry string){
		"writable-parent": func(t *testing.T, _ syncHost, entry string) {
			t.Helper()
			run(t, "", "chmod", "g+w", filepath.Dir(entry))
		},
		"nonroot-owner": func(t *testing.T, _ syncHost, entry string) { t.Helper(); podmanChown(t, "1", entry) },
		"file-link":     func(t *testing.T, h syncHost, entry string) { t.Helper(); run(t, "", "ln", entry, h.at("alias")) },
		"acl": func(t *testing.T, h syncHost, _ string) {
			t.Helper()
			userWriteACL(t, h.at("usr-local-libexec/hololive-bot"))
		},
	}

	for name, attack := range attacks {
		h := syncHost(p.e.clone(t, string(p.cardHost), "host-source-unsafe-"+name))
		attack(t, h, h.at("opt/current/scripts/deploy/quadlet-redeploy-service.sh"))
		p.e.rejects(t, h, p.sourceRepo, name, "unsafe installed coordinator authority", "--source-only", installed)
	}
}

func (p *poolWindow) assertPartialPublication(t *testing.T, installed, next string) {
	t.Helper()

	h := syncHost(p.e.clone(t, string(p.cardHost), "host-source-partial"))
	put(t, h.at("events.fail-before"), "/opt/current/scripts/deploy/sync-opt-current.sh\n")
	harness.Fails(t, "partial publication", p.e.sync(t, h, p.sourceRepo, "--source-only", installed))

	if !sameFile(t, filepath.Join(p.sourceRepo, "hololive/hololive-api/VERSION"), h.at("opt/current/hololive/hololive-api/VERSION")) ||
		!sameFile(t, filepath.Join(p.w2, "scripts/deploy/sync-opt-current.sh"), h.at("opt/current/scripts/deploy/sync-opt-current.sh")) {
		t.Fatal("partial publication did not stop at the injected file")
	}

	if got := strings.TrimSpace(harness.Read(t, h.at("opt/current/.source-revision"))); got != "incomplete "+installed+" -> "+next {
		t.Fatalf("partial publication stamp = %q", got)
	}

	found := backups(t, h, shortHead(t, p.sourceRepo)+"-")
	if len(found) == 0 || !sameFile(t, filepath.Join(p.w2, "hololive/hololive-api/VERSION"), filepath.Join(found[0], "hololive/hololive-api/VERSION")) {
		t.Fatal("partial publication lost its recovery point")
	}

	p.e.rejects(t, h, p.sourceRepo, "partial publication replay", "installed revision mismatch", "--source-only", installed)
}

func (p *poolWindow) assertSourceOnlyPublication(t *testing.T, installed, next string) {
	t.Helper()

	h := p.cardHost
	protected := func() string {
		r := harness.Bash(t, harness.Cmd{
			Script: `stat -c '%i %u:%g %a %y %z' "$1" "$2"`,
			Args:   []string{h.installed(filepath.Base(migratorPath)), h.at("opt/current/" + migratorPath)},
		})
		harness.Succeeds(t, "migrator metadata", r)

		return protectedSnapshot(t, h) + r.Stdout
	}

	before := protected()

	p.e.syncs(t, h, p.sourceRepo, "source-only", "--source-only", installed)

	if protected() != before {
		t.Fatal("source-only replaced protected metadata or bytes")
	}

	if strings.Contains("\n"+h.events(t), "\nsystemctl ") {
		t.Fatal("source-only invoked systemctl")
	}

	if strings.TrimSpace(harness.Read(t, h.at("opt/current/.source-revision"))) != next {
		t.Fatal("source revision not published")
	}

	found := backups(t, h, shortHead(t, p.sourceRepo)+"-")
	if len(found) != 1 || strings.TrimSpace(harness.Read(t, filepath.Join(found[0], ".source-revision"))) != installed {
		t.Fatalf("missing source recovery point: %v", found)
	}

	for _, path := range []string{"scripts/deploy/sync-opt-current.sh", "hololive/hololive-api/VERSION"} {
		if !sameFile(t, filepath.Join(p.sourceRepo, path), h.at("opt/current/"+path)) || !sameFile(t, filepath.Join(p.w2, path), filepath.Join(found[0], path)) {
			t.Errorf("%s was not published with a recovery copy", path)
		}
	}
}

func (p *poolWindow) assertApplicationOnlyDrift(t *testing.T) {
	t.Helper()

	e := p.e

	for i, path := range []string{
		"opt/current/deploy/postgres/pg_hba.conf",
		"opt/current/hololive/hololive-api/scripts/init-db/00-assert-pg19-runtime.sql",
		"etc-containers-systemd/hololive-bot/hololive-api.container",
		"etc-containers-systemd/hololive-bot/holo-postgres.container",
		"etc-containers-systemd/hololive-bot/hololive-alarm-worker.container",
		"etc-containers-systemd/hololive-bot/hololive-api.container.d/40-x-spaces.conf",
		"usr-local-libexec/hololive-bot/quadlet-prestart",
		"etc-nftables.d/admin-dashboard-ingress.nft",
	} {
		h := syncHost(e.clone(t, string(p.w1Host), "host-drift-"+itoa(uint64(i)))) // #nosec G115 -- 작은 양수 인덱스다.
		appendFile(t, h.at(path), "\n# drift\n")
		e.rejects(t, h, p.w2, path, "application-only", "--application-only")
	}

	override := syncHost(e.clone(t, string(p.w1Host), "host-override"))
	put(t, override.installed("hololive-db-migrate.container.d/60-command.conf"), "[Container]\nExec=--configure-login-roles\n")
	e.rejects(t, override, p.w2, "migrator override", "requires the schema-only migrator", "--application-only")
	e.rejects(t, p.w1Host, p.w1, "pool candidate", "requires the schema-only migrator", "--application-only")

	drift := e.clone(t, p.w2, "repo-infra-change")
	put(t, filepath.Join(drift, "deploy/postgres/unreviewed.conf"), "drift\n")
	commit(t, drift, "changed-infrastructure")
	e.rejects(t, p.w1Host, drift, "candidate infrastructure", "application-only tree drift", "--application-only")
}

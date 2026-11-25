package pocentral

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const (
	oldRevision       = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newRevision       = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	change            = "20261003T000000Z"
	runTimeout        = 60 * time.Second
	primaryHost       = "holo-postgres"
	primaryDB         = "hololive"
	modePreflight     = "preflight"
	modeQuiesce       = "quiesce"
	modeResume        = "resume"
	rootfsManifest    = "rootfs-manifest.json"
	collectorManifest = "collector-manifest.json"
)

var (
	oldID = "sha256:" + strings.Repeat("a", 64)
	newID = "sha256:" + strings.Repeat("b", 64)
)

type source struct {
	central string
	slots   map[string]map[string]string
}

var loadSource = sync.OnceValues(func() (source, error) {
	root, err := os.Getwd()
	if err != nil {
		return source{}, fmt.Errorf("locate repository root: %w", err)
	}

	root = filepath.Join(root, "..", "..", "..")

	central, err := os.ReadFile(filepath.Join(root, "scripts/deploy/lib/po-central-remote.sh")) // #nosec G304 -- 저장소 안의 고정 경로다.
	if err != nil {
		return source{}, fmt.Errorf("read po-central-remote.sh: %w", err)
	}

	table, err := os.ReadFile(filepath.Join(root, "scripts/deploy/lib/collector-slots.tsv")) // #nosec G304 -- 저장소 안의 고정 경로다.
	if err != nil {
		return source{}, fmt.Errorf("read collector-slots.tsv: %w", err)
	}

	rows := strings.Split(strings.TrimSpace(string(table)), "\n")
	header := strings.Split(rows[0], "\t")
	slots := map[string]map[string]string{}

	for _, row := range rows[1:] {
		fields := strings.Split(row, "\t")
		if len(fields) != len(header) {
			return source{}, errors.New("collector-slots.tsv row width differs from its header")
		}

		slot := map[string]string{}

		for i, name := range header {
			slot[name] = fields[i]
		}

		slots[fields[0]] = slot
	}

	return source{central: string(central), slots: slots}, nil
})

func load(t *testing.T) source {
	t.Helper()

	s, err := loadSource()
	if err != nil {
		t.Fatalf("load po-central sources: %v", err)
	}

	return s
}

func function(t *testing.T, text, name string) string {
	t.Helper()

	match := regexp.MustCompile("(?ms)^" + regexp.QuoteMeta(name) + "\\(\\) \\{\n.*?^\\}").FindString(text)
	if match == "" {
		t.Fatalf("function %s not found", name)
	}

	return match
}

func between(t *testing.T, text, from, to string) string {
	t.Helper()

	start := strings.Index(text, from)
	if start < 0 {
		t.Fatalf("anchor %q not found", from)
	}

	if to == "" {
		return text[start:]
	}

	end := strings.Index(text, to)
	if end < 0 {
		t.Fatalf("anchor %q not found", to)
	}

	return text[start:end]
}

func slotPreamble(t *testing.T, slot string) string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.sh"; collector_slot_preamble "$1"`, Args: []string{slot}})
	harness.Succeeds(t, "slot preamble "+slot, r)

	return r.Stdout
}

func run(t *testing.T, body, state, fixtureCase string, args ...string) harness.Result {
	t.Helper()

	if args == nil {
		args = []string{harness.Root(t)}
	}

	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", append([]string{"-s", "--", state, fixtureCase}, args...)...) // #nosec G204 -- 테스트 fixture 본문만 실행한다.

	cmd.Stdin = strings.NewReader(body)
	cmd.Dir = harness.Root(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: strings.HasSuffix(fixtureCase, "_group_TERM")}

	var stdout, stderr strings.Builder

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r := harness.Result{}

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			t.Fatalf("%s: fixture did not finish within %s\nstdout:\n%s\nstderr:\n%s", fixtureCase, runTimeout, stdout.String(), stderr.String())
		}

		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("run fixture: %v", err)
		}

		r.ExitCode = exitErr.ExitCode()
	}

	r.Stdout, r.Stderr = stdout.String(), stderr.String()

	return r
}

func require(t *testing.T, ok bool, message string, r harness.Result) {
	t.Helper()

	if !ok {
		t.Fatalf("%s\n%s", message, r)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func TestLeaseMigrationProofQueriesOnlyThePrivatePrimaryRoute(t *testing.T) {
	t.Parallel()

	s := load(t)
	setup := proofSetup + "\n" + function(t, s.central, "collector_database_endpoint") + "\n" + function(t, s.central, "require_collector_lease_migration")

	for _, c := range []struct {
		name, host, port, database string
		success, queries           bool
	}{
		{"direct", primaryHost, "5432", primaryDB, true, true},
		{"other_pooler", "other-pooler", "6000", primaryDB, false, false},
		{"foreign", "other-postgres", "5432", primaryDB, false, false},
		{"wrong_port", primaryHost, "6000", primaryDB, false, false},
		{"wrong_database", primaryHost, "5432", "other", false, false},
		{"socket_override", primaryHost, "5432", primaryDB, false, false},
		{"ambiguous_host", primaryHost, "5432", primaryDB, false, false},
		{"query_failure", primaryHost, "5432", primaryDB, false, true},
		{"writable_proof", primaryHost, "5432", primaryDB, false, true},
		{"wrong_checksum", primaryHost, "5432", primaryDB, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			state := t.TempDir()
			env := "POSTGRES_HOST=" + c.host + "\nPOSTGRES_PORT=" + c.port + "\nPOSTGRES_DB=" + c.database + "\n"

			switch c.name {
			case "socket_override":
				env += "POSTGRES_SOCKET_PATH=/other\n"
			case "ambiguous_host":
				env += "POSTGRES_HOST=other\n"
			}

			harness.Write(t, filepath.Join(state, "collector.env"), env)

			r := run(t, setup+"\nrequire_collector_lease_migration applied\n", state, c.name)
			require(t, (r.ExitCode == 0) == c.success, "primary route proof outcome", r)

			queryArgs := filepath.Join(state, "query-args")
			require(t, exists(queryArgs) == c.queries, "query only after route validation", r)

			if !c.queries {
				return
			}

			args := strings.Split(strings.TrimSuffix(harness.Read(t, queryArgs), "\n"), "\n")

			for _, pair := range [][2]string{
				{"--network", "none"},
				{"--volume", "hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro"},
				{"--env", "PGHOST=/var/run/postgresql"},
				{"--env", "PGPORT=5432"},
				{"--env", "PGUSER=hololive_migrator"},
				{"--env", "PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s"},
				{"-v", "ON_ERROR_STOP=1"},
				{"-f", "-"},
			} {
				found := false

				for i := range len(args) - 1 {
					if args[i] == pair[0] && args[i+1] == pair[1] {
						found = true
					}
				}

				require(t, found, "private read-only ledger transport "+pair[1], r)
			}
		})
	}
}

func TestPostActivationEndpointAcceptsOnlyTheSlotPrimary(t *testing.T) {
	t.Parallel()

	s := load(t)

	for _, c := range []struct {
		slot, host, port string
		accepted         bool
	}{
		{"c", primaryHost, "5432", true},
		{"c", "other-pooler", "6000", false},
		{"c", "foreign", "5432", false},
		{"b", "100.100.1.8", "5433", true},
		{"b", "100.100.1.8", "6001", false},
		{"b", "100x100x1x8", "5433", false},
	} {
		t.Run(c.slot+"/"+c.host+":"+c.port, func(t *testing.T) {
			t.Parallel()

			state := t.TempDir()
			harness.Write(t, filepath.Join(state, "collector.env"), "POSTGRES_HOST="+c.host+"\nPOSTGRES_PORT="+c.port+"\nPOSTGRES_DB=hololive\n")

			body := proofSetup + "\n" + function(t, s.central, "collector_database_endpoint") + "\n" +
				"\nslot=" + c.slot + "\n" + function(t, s.central, "require_collector_database_endpoint") + "\nrequire_collector_database_endpoint\n"
			r := run(t, body, state, "endpoint")
			require(t, (r.ExitCode == 0) == c.accepted, "post-activation database endpoint", r)
		})
	}
}

func TestResumeRequiresItsOwnCardBackupAndStaging(t *testing.T) {
	t.Parallel()

	s := load(t)
	prefix := between(t, s.central, "", "install -d -m 0755 /run/lock")

	for _, slot := range []string{"c", "b"} {
		remote := s.slots[slot]["root"]

		for _, c := range []struct {
			prefix, stagedAt string
			accepted         bool
		}{
			{"card-po", change, true},
			{"po", change, false},
			{"card-po", "20261002T000000Z", false},
		} {
			t.Run(slot+"/"+c.prefix+"/"+c.stagedAt, func(t *testing.T) {
				t.Parallel()

				args := []string{slot, modeResume, remote + "/po-staging-" + c.stagedAt, remote + "/backups/" + c.prefix + "-" + slot + "-" + change, newRevision, "6.0.1", "-"}
				r := run(t, slotPreamble(t, slot)+"set -- "+strings.Join(args, " ")+"\n"+prefix, harness.Root(t), "paths")
				require(t, (r.ExitCode == 0) == c.accepted, "W2 resume requires its card backup and staging", r)
			})
		}
	}
}

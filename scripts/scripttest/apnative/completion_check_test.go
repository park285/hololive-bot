package apnative

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const completionFakeSSH = `remote_cmd="${@: -1}"
if [[ "$remote_cmd" == sudo\ -n\ python3\ -\ verify-native\ * ]]; then
  cat >/dev/null
  exit 0
fi
[[ "$remote_cmd" == bash\ -s\ --* ]] || { echo "unexpected ssh command: $remote_cmd" >&2; exit 99; }
args="${remote_cmd#bash -s --}"
eval "set -- $args"
PATH="$FAKE_REMOTE_BIN:$PATH" bash -s -- "$@"
`

const completionFakeSudo = `args=()
while (($# > 0)); do
  case "$1" in
    -n) shift ;;
    -u) shift 2 ;;
    *) args+=("$1"); shift ;;
  esac
done
set -- "${args[@]}"
case "${1:-}" in
  test|grep) exit 0 ;;
  cat)
    [[ "$2" == /opt/hololive-bot/collector/current/rollback-contract/po-unit-presence ]] || exit 1
    printf 'absent\n'
    ;;
  env)
    shift
    while (($# > 0)); do case "$1" in *=*) shift ;; *) break ;; esac; done
    if [[ "${1:-}" == */bin/healthcheck ]]; then
      if [[ " $* " == *" --body "* ]]; then
        printf '{"status":"ready","helper":"ok","first_success":%s,"handoff_status":"%s","handoff_processed":true,"due_jobs":0,"pending_queue":0,"version":"test"}\n' \
          "${MOCK_FIRST_SUCCESS:-true}" "${MOCK_HANDOFF_STATUS:-PROCESSED}"
      fi
      exit 0
    fi
    exec "$@"
    ;;
  *) exec "$@" ;;
esac
`

const completionFakeSystemctl = `if [[ "${1:-}" == is-active ]]; then exit 0; fi
[[ "${1:-}" == show ]] || exit 1
prop=""
for arg in "$@"; do
  case "$arg" in ActiveState|SubState|NRestarts|ActiveEnterTimestamp|EnvironmentFiles|LoadState) prop="$arg" ;; esac
done
case "$prop" in
  ActiveState) echo active ;;
  LoadState) echo not-found ;;
  SubState) echo running ;;
  NRestarts) echo 0 ;;
  ActiveEnterTimestamp) echo "Tue 2026-06-30 08:14:12 UTC" ;;
  EnvironmentFiles) echo "${MOCK_ENVIRONMENT_FILES:-/etc/stack-secrets/hololive-bot/collector.env /etc/hololive-bot/collector-host.env}" ;;
  *) exit 1 ;;
esac
`

const completionFakeJournal = `[[ "${MOCK_JOURNAL_FAILURE:-false}" != true ]] || exit 1
cat <<'LOG'
Jun 30 08:14:13 host collector-wrapper[1]: 2026-06-30T08:14:13Z INF logging/log.go:44 ingestion runtime configured active_active_enabled=true
Jun 30 08:14:13 host collector-wrapper[1]: 2026-06-30T08:14:13Z INF cache/service_connection.go:63 Cache store connected
Jun 30 08:14:13 host collector-wrapper[1]: 2026-06-30T08:14:13Z INF dbx/client.go:199 postgres_pool_connected
LOG
`

const completionQUICBuffer = `required="${1:?required}"
host_name="${2:?host}"
[[ "$required" == 7500000 ]]
echo "AP QUIC UDP buffers ok on ${host_name}: runtime rmem=7500000 wmem=7500000; persisted rmem=7500000 wmem=7500000"
`

var completionLibs = []string{
	"ap-completion-check.sh",
	"lib/ap-host.sh",
	"lib/youtubejs-node-version.sh",
	"lib/ap-collector-readiness.sh",
	"lib/ap-host-native-po.sh",
	"lib/worker-profile-pair.py",
	"lib/collector-slots.sh",
	"lib/collector-slots.tsv",
}

type completionFixture struct {
	root, bin string
}

func copyRepoFile(t *testing.T, rel, dst string) {
	t.Helper()

	data, err := os.ReadFile(harness.Path(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	harness.Write(t, dst, string(data))

	if strings.HasSuffix(rel, ".sh") {
		if err := os.Chmod(dst, 0o700); err != nil { // #nosec G302 -- 복사한 저장소 스크립트를 실행해야 한다.
			t.Fatal(err)
		}
	}
}

func newCompletionFixture(t *testing.T) completionFixture {
	t.Helper()

	dir := t.TempDir()
	f := completionFixture{root: filepath.Join(dir, "repo"), bin: filepath.Join(dir, "bin")}

	for _, rel := range completionLibs {
		copyRepoFile(t, "scripts/deploy/"+rel, filepath.Join(f.root, "scripts/deploy", rel))
	}

	harness.Executable(t, filepath.Join(f.root, "scripts/deploy/lib/require-quic-udp-buffer.sh"), completionQUICBuffer)
	harness.Write(t, filepath.Join(f.root, "KR.key"), "")
	harness.Write(t, filepath.Join(f.root, "scripts/deploy/ap-hosts/osaka.conf"),
		strings.NewReplacer("unreachable.invalid", "mock-host", "ap-osaka", "mock-host").Replace(osakaConf))

	for name, body := range map[string]string{
		"sleep":      `printf '%s\n' "$*" >> "$FAKE_DELAY_LOG"` + "\n",
		"node":       "[[ \"${1:-}\" == --version ]] || exit 1\necho v26.12.1\n",
		"ssh":        completionFakeSSH,
		"sudo":       completionFakeSudo,
		"systemctl":  completionFakeSystemctl,
		"journalctl": completionFakeJournal,
	} {
		harness.Executable(t, filepath.Join(f.bin, name), body)
	}

	return f
}

func (f completionFixture) check(t *testing.T, env ...string) harness.Result {
	t.Helper()

	base := make([]string, 0, 6+len(env))

	base = append(base,
		"FAKE_REMOTE_BIN="+f.bin,
		"REPO_ROOT="+f.root,
		"SSH_KEY="+filepath.Join(f.root, "KR.key"),
		"PO_EXPECTED_PRESENCE=absent",
		"FAKE_DELAY_LOG="+filepath.Join(f.root, "delays"),
		"CHANGE_STARTED_AT=2026-06-30T08:13:49Z",
	)

	return harness.Bash(t, harness.Cmd{
		Script: `exec "$REPO_ROOT/scripts/deploy/ap-completion-check.sh" osaka`,
		Env:    append(base, env...),
		Bin:    f.bin,
	})
}

func TestCompletionCheckAcceptsAReadyCollector(t *testing.T) {
	t.Parallel()

	r := newCompletionFixture(t).check(t)
	harness.Succeeds(t, "ready collector with issuer-absent rollback", r)

	if !strings.Contains(r.Stdout, "collector AP completion check passed") {
		t.Fatalf("completion check did not report success:\n%s", r)
	}
}

func TestCompletionCheckRejectsIncompleteCutovers(t *testing.T) {
	t.Parallel()

	f := newCompletionFixture(t)
	cases := map[string]string{
		"expected issuer missing":   "PO_EXPECTED_PRESENCE=present",
		"first success pending":     "MOCK_FIRST_SUCCESS=false",
		"API handoff pending":       "MOCK_HANDOFF_STATUS=PENDING",
		"shared compose env loaded": "MOCK_ENVIRONMENT_FILES=/etc/stack-secrets/hololive-bot/ap-compose.env /etc/stack-secrets/hololive-bot/collector.env /etc/hololive-bot/collector-host.env",
		"journal unreadable":        "MOCK_JOURNAL_FAILURE=true",
	}

	for name, env := range cases {
		harness.Fails(t, name, f.check(t, env))
	}
}

func readiness(t *testing.T, script string, args ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/ap-collector-readiness.sh"` + "\n" + script,
		Args:   args,
		Env:    []string{"ATTEMPTS=" + filepath.Join(t.TempDir(), "attempts")},
	})
}

const readyPayload = `{"status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED","handoff_processed":true,"pending_queue":0}`

func TestCollectorReadinessPollRetriesUntilReady(t *testing.T) {
	t.Parallel()

	r := readiness(t, `probe() {
  local attempt=0
  [[ ! -r "$ATTEMPTS" ]] || attempt="$(cat "$ATTEMPTS")"
  attempt=$((attempt + 1))
  printf '%s\n' "$attempt" >"$ATTEMPTS"
  if ((attempt == 1)); then printf '%s\n' '{"status":"not_ready","dependency":"postgres_queue"}'; else printf '%s\n' "$1"; fi
}
collector_readiness_poll 2 0 probe "$1"
`, readyPayload)

	harness.Succeeds(t, "poll", r)

	if strings.TrimSpace(r.Stdout) != readyPayload {
		t.Errorf("poll returned %q, want the ready payload", r.Stdout)
	}
}

func TestCollectorReadinessRejectsMalformedPayloads(t *testing.T) {
	t.Parallel()

	for _, payload := range []string{
		"not-json",
		`{"status":"ready" "helper":"ok","first_success":true,"handoff_status":"PROCESSED","pending_queue":0}`,
		`{"status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED"}`,
		`{"status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED","pending_queue":}`,
		`{"status":"ready","helper":"ok","first_success":true,"handoff_status":"PROCESSED","pending_queue":0}`,
	} {
		harness.Fails(t, payload, readiness(t, `collector_readiness_validate "$1"`, payload))
	}
}

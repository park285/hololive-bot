package logscripts

import (
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const remoteSync = `unset HOL_LOG_CENTRAL_LOG_DIR HOL_LOG_CENTRAL_SERVICES HOL_LOG_OSAKA_LOG_DIR HOL_LOG_OSAKA_SERVICES
source "$SCRIPTTEST_ROOT/scripts/logs/remote-sync-main-logs.sh" >/dev/null
`

func remoteSyncLines(t *testing.T, body string) []string {
	t.Helper()

	r := harness.Bash(t, harness.Cmd{Script: remoteSync + body})
	harness.Succeeds(t, body, r)

	return strings.Split(strings.TrimRight(r.Stdout, "\n"), "\n")
}

func TestRemoteSyncDefaults(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`printf '%s\n' "$CENTRAL_REMOTE_LOG_DIR"`:   "/var/log/hololive-bot",
		`printf '%s\n' "$CENTRAL_SERVICES"`:         "hololive-api alarm-worker collector-c",
		`printf '%s\n' "$OSAKA_REMOTE_LOG_DIR"`:     "/var/log/hololive-bot",
		`printf '%s\n' "$OSAKA_SERVICES"`:           "collector-a",
		`remote_log_service_name osaka collector-a`: "collector-a",
		`remote_log_service_name seoul collector-b`: "collector-b",
	}
	for body, want := range cases {
		t.Run(body, func(t *testing.T) {
			t.Parallel()

			if got := strings.Join(remoteSyncLines(t, body), "\n"); got != want {
				t.Errorf("%s = %q, want %q", body, got, want)
			}
		})
	}
}

func TestRemoteSyncIncludesRotatedAndArchivedLogs(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"central hololive-api": {"hololive-api.log.*"},
		"central collector-c":  {"archive/collector-c*"},
		"osaka collector-a":    {"collector-a.log.*", "archive/collector-a*"},
	}
	for args, want := range cases {
		t.Run(args, func(t *testing.T) {
			t.Parallel()

			got := remoteSyncLines(t, "remote_log_include_patterns "+args)

			for _, pattern := range want {
				if !slices.Contains(got, pattern) {
					t.Errorf("remote_log_include_patterns %s = %q, missing %q", args, got, pattern)
				}
			}
		})
	}
}

func TestRemoteSyncStatusExposesOnlyMirrorNamespace(t *testing.T) {
	t.Parallel()

	if got := strings.Join(remoteSyncLines(t, "status_remote central"), "\n"); strings.Contains(got, "main=") {
		t.Errorf("status_remote central exposed the live runtime namespace: %q", got)
	}
}

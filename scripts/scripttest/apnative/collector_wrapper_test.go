package apnative

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const collectorWrapper = "scripts/deploy/lib/ap-host-native-collector-wrapper.sh"

func runWrapper(t *testing.T, target string, env ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `exec env -i PATH="$PATH" "$@" "$SCRIPTTEST_ROOT/$WRAPPER" "$TARGET"`,
		Args:   env,
		Env:    []string{"WRAPPER=" + collectorWrapper, "TARGET=" + target},
	})
}

func credentialProbe(t *testing.T) string {
	t.Helper()

	probe := filepath.Join(t.TempDir(), "credential-probe")
	harness.Executable(t, probe, `printf '%s|%s|%s\n' "${POSTGRES_USER-}" "${POSTGRES_DB-}" "${POSTGRES_PASSWORD-}"`+"\n")

	return probe
}

func TestCollectorWrapperResolvesTheCollectorRoleCredential(t *testing.T) {
	t.Parallel()

	probe := credentialProbe(t)
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"default role", []string{"HOLOLIVE_COLLECTOR_A_PASSWORD=collector", "HOLOLIVE_DB_PASSWORD=runtime", "DB_PASSWORD=legacy"}, "hololive_collector_a|hololive|collector"},
		{"explicit role", []string{"POSTGRES_USER=hololive_collector_a", "HOLOLIVE_COLLECTOR_A_PASSWORD=collector", "HOLOLIVE_DB_PASSWORD=runtime"}, "hololive_collector_a|hololive|collector"},
		{"explicit password wins", []string{"POSTGRES_USER=hololive_collector_a", "POSTGRES_PASSWORD=explicit", "HOLOLIVE_COLLECTOR_A_PASSWORD=collector", "HOLOLIVE_DB_PASSWORD=runtime"}, "hololive_collector_a|hololive|explicit"},
	}

	for _, tc := range cases {
		r := runWrapper(t, probe, tc.env...)
		harness.Succeeds(t, tc.name, r)

		if got := strings.TrimSpace(r.Stdout); got != tc.want {
			t.Errorf("%s: credential = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCollectorWrapperRejectsUnresolvedCredentials(t *testing.T) {
	t.Parallel()

	probe := credentialProbe(t)
	cases := map[string][]string{
		"noncanonical role":            {"POSTGRES_USER=custom_scraper", "POSTGRES_PASSWORD=explicit", "HOLOLIVE_COLLECTOR_A_PASSWORD=collector"},
		"admin password is not a role": {"POSTGRES_USER=other", "DB_PASSWORD=legacy"},
		"no runtime credential borrow": {"POSTGRES_USER=hololive_scraper", "HOLOLIVE_DB_PASSWORD=runtime", "DB_PASSWORD=legacy"},
		"missing collector password":   {"POSTGRES_USER=hololive_collector_a"},
	}

	for name, env := range cases {
		harness.Fails(t, name, runWrapper(t, probe, env...))
	}
}

func TestCollectorWrapperPreservesTheTargetExitStatus(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "credential-failure")
	harness.Executable(t, target, "exit 23\n")

	if r := runWrapper(t, target, "HOLOLIVE_COLLECTOR_A_PASSWORD=collector"); r.ExitCode != 23 {
		t.Fatalf("wrapper exit = %s, want 23", r)
	}
}

package deployenv

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const composeEnvValidate = `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/compose-env.sh"; compose_env_validate_file_format "$1"`

func validateEnvFile(t *testing.T, path string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{Script: composeEnvValidate, Args: []string{path}})
}

func TestComposeEnvAcceptsLiteralFiles(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"valid env":                            "DB_PASSWORD=db-secret\nCACHE_PASSWORD=cache-secret\nCOLLECTOR_RUNTIME_ALLOWED=false\n",
		"CRLF, empty values and quoted spaces": "KEY=value\r\nEMPTY=\r\nQUOTED=\"two words\"\r\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "env")
			harness.Write(t, path, content)
			harness.Succeeds(t, name, validateEnvFile(t, path))
		})
	}
}

func TestComposeEnvListsSortedKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "env")
	harness.Write(t, path, "DB_PASSWORD=db-secret\nCACHE_PASSWORD=cache-secret\nALARM_DISPATCH_RETENTION_LIMIT=1000\nALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH=1000\nCOLLECTOR_RUNTIME_ALLOWED=false\n")

	r := harness.Bash(t, harness.Cmd{
		Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/compose-env.sh"; compose_env_list_keys_from_file "$1"`,
		Args:   []string{path},
	})
	harness.Succeeds(t, "list keys", r)

	want := "ALARM_DISPATCH_MAX_DELIVERIES_PER_BATCH ALARM_DISPATCH_RETENTION_LIMIT CACHE_PASSWORD COLLECTOR_RUNTIME_ALLOWED DB_PASSWORD"
	if got := strings.Join(strings.Fields(r.Stdout), " "); got != want {
		t.Errorf("compose_env_list_keys_from_file = %q, want %q", got, want)
	}
}

func TestComposeEnvKeyExistsMatchesWholeKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "env")
	harness.Write(t, path, "ABC=1\r\nEMPTY=\n#COMMENTED=1\n")

	cases := map[string]bool{"ABC": true, "EMPTY": true, "AB": false, "ABCD": false, "A.C": false, "COMMENTED": false}
	for key, exists := range cases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, harness.Cmd{
				Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/compose-env.sh"; compose_env_key_exists_in_file "$1" "$2"`,
				Args:   []string{path, key},
			})

			if exists {
				harness.Succeeds(t, key, r)
			} else {
				harness.Fails(t, key, r)
			}
		})
	}
}

func TestComposeEnvRejectsUnsafeContent(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"leading export":            "export DB_PASSWORD=x\n",
		"command substitution":      "DB_PASSWORD=$(cat /secret)\n",
		"invalid key":               "DB-PASSWORD=x\n",
		"missing equals":            "DB_PASSWORD\n",
		"key whitespace":            " DB_PASSWORD=x\n",
		"control character":         "DB_PASSWORD=a\tb\n",
		"duplicate key":             "DB_PASSWORD=a\nDB_PASSWORD=b\n",
		"value leading whitespace":  "KEY= leading\n",
		"value trailing whitespace": "KEY=trailing \n",
		"value embedded control":    "KEY=embedded\x01control\n",
		"value backticks":           "KEY=`id`\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "env")
			harness.Write(t, path, content)
			harness.Fails(t, name, validateEnvFile(t, path))
		})
	}
}

func TestComposeEnvRejectsNonRegularFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.env")
	harness.Write(t, valid, "DB_PASSWORD=x\n")
	harness.Write(t, filepath.Join(dir, "empty.env"), "")
	harness.Symlink(t, valid, filepath.Join(dir, "link.env"))

	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.env"), 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	for _, name := range []string{"empty.env", "link.env", "pipe.env", ".", "missing.env"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			harness.Fails(t, name, validateEnvFile(t, filepath.Join(dir, name)))
		})
	}
}

func TestComposeEnvDiagnosticsOmitInvalidKeyInput(t *testing.T) {
	t.Parallel()

	for _, line := range []string{"synthetic-sensitive-key-token=value", " synthetic-sensitive-key-token=value"} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "env")
			harness.Write(t, path, line+"\n")

			r := validateEnvFile(t, path)
			harness.Fails(t, "invalid key", r)

			if strings.Contains(r.Stdout+r.Stderr, "synthetic-sensitive-key-token=value") {
				t.Errorf("diagnostics exposed the rejected input: %s", r)
			}
		})
	}
}

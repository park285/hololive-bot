package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func TestRepoAPDeployScriptsUseSplitRuntimeEnv(t *testing.T) {
	for _, file := range []string{
		"scripts/deploy/ap-deploy.sh",
		"scripts/deploy/ap-completion-check.sh",
		"scripts/deploy/ap-rollback.sh",
		"scripts/deploy/ap-collector-preflight.sh",
	} {
		content := readRepoFile(t, file)
		if strings.Contains(content, "/etc/stack-secrets/hololive-bot/env") {
			t.Fatalf("%s still references monolithic /etc/stack-secrets/hololive-bot/env", file)
		}

		if !strings.Contains(content, "/etc/stack-secrets/hololive-bot/ap-compose.env") {
			t.Fatalf("%s missing AP-safe compose env file contract", file)
		}
	}
}

func TestRepoAPDeployScriptsRequirePersistedQUICUDPBuffers(t *testing.T) {
	lib := readRepoFile(t, "scripts/deploy/lib/require-quic-udp-buffer.sh")

	for _, snippet := range []string{
		"net.core.rmem_max",
		"net.core.wmem_max",
		"/etc/sysctl.d/*.conf",
		"are not persisted",
	} {
		if !strings.Contains(lib, snippet) {
			t.Fatalf("require-quic-udp-buffer.sh missing runtime+persisted contract %q", snippet)
		}
	}

	for _, file := range []string{
		"scripts/deploy/ap-collector-preflight.sh",
		"scripts/deploy/ap-completion-check.sh",
	} {
		content := readRepoFile(t, file)
		if !strings.Contains(content, "require-quic-udp-buffer.sh") {
			t.Fatalf("%s must delegate QUIC UDP buffer checks to require-quic-udp-buffer.sh", file)
		}

		if strings.Contains(content, "sysctl -n net.core.rmem_max") {
			t.Fatalf("%s still uses runtime-only inline sysctl check", file)
		}
	}
}

// accepted-risk ledger는 verify-full 전환으로 종료되었다. 문서가 다시 생기거나
// compose 어디든 POSTGRES_SSLMODE_ALLOW_INSECURE가 재등장하면 회귀다.
func TestRepoPostgresSSLModeInsecureDowngradeIsRetired(t *testing.T) {
	root := repoRootFromConfigTest(t)
	ledgerPath := filepath.Join(root, "docs", "current", "security", "accepted-risk-ap-postgres-sslmode.md")

	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatalf("accepted-risk-ap-postgres-sslmode.md still exists; the ledger exits with the verify-full transition (stat err=%v)", err)
	}

	composeDir := filepath.Join(root, "deploy", "compose")

	entries, err := os.ReadDir(composeDir)
	if err != nil {
		t.Fatalf("read compose dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		content := readRepoFile(t, filepath.Join("deploy", "compose", entry.Name()))
		if strings.Contains(content, "POSTGRES_SSLMODE_ALLOW_INSECURE") {
			t.Fatalf("deploy/compose/%s still references POSTGRES_SSLMODE_ALLOW_INSECURE; verify-full replaced the downgrade path", entry.Name())
		}
	}
}

// 모든 운영 스택 렌더에서 Postgres 클라이언트는 verify-full + 마운트된 CA 번들을
// 사용해야 한다(구 accepted-risk ledger의 exit criteria).
func TestRepoComposeAllStacksRenderVerifyFullPostgres(t *testing.T) {
	stacks := []struct {
		name     string
		files    []string
		services []string
	}{
		{
			name:     "base prod",
			files:    []string{composeProdFile},
			services: []string{serviceHololiveAPI, serviceAlarmWorker, load.RuntimeYouTubeCollector},
		},
		{
			name: "live-compat",
			files: []string{
				composeProdFile,
				composeLiveCompatFile,
			},
			services: []string{serviceHololiveAPI, serviceAlarmWorker, load.RuntimeYouTubeCollector},
		},
		{
			name: "main-ap live-compat",
			files: []string{
				composeProdFile,
				composeLiveCompatFile,
				"deploy/compose/docker-compose.main-ap.yml",
				"deploy/compose/docker-compose.main-ap.live-compat.yml",
			},
			services: []string{load.RuntimeYouTubeCollector},
		},
	}

	for _, tt := range stacks {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			for _, service := range tt.services {
				env := composeEnvironment(t, cfg, service)
				if env["POSTGRES_SSLMODE"] != load.PostgresSSLModeVerifyFull {
					t.Fatalf("%s in %s POSTGRES_SSLMODE = %q, want verify-full", service, tt.name, env["POSTGRES_SSLMODE"])
				}

				if value, ok := env["POSTGRES_SSLMODE_ALLOW_INSECURE"]; ok {
					t.Fatalf("%s in %s renders retired POSTGRES_SSLMODE_ALLOW_INSECURE=%q", service, tt.name, value)
				}

				if env["POSTGRES_SSLROOTCERT"] != postgresCACertPath {
					t.Fatalf("%s in %s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", service, tt.name, env["POSTGRES_SSLROOTCERT"])
				}
			}
		})
	}
}

// holo-postgres는 server TLS를 켠 채로 기동해야 verify-full 클라이언트가 성립한다.
// 이때 server key는 클라이언트들이 통째로 마운트하는 certs/ 디렉토리 밖(postgres-tls/)에 둔다.
func TestRepoComposeHoloPostgresServesTLS(t *testing.T) {
	for _, tt := range []struct {
		name  string
		files []string
	}{
		{name: "base prod", files: []string{composeProdFile}},
		{name: "live-compat", files: []string{
			composeProdFile,
			composeLiveCompatFile,
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			assertHoloPostgresTLSCommand(t, cfg, tt.name)
			assertHoloPostgresTLSMount(t, cfg, tt.name)
			assertDBMigrateVerifyFullTLS(t, cfg, tt.name)
		})
	}
}

func assertHoloPostgresTLSCommand(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	command := composeCommand(t, cfg, serviceHoloPostgres)

	for _, flag := range []string{
		"ssl=on",
		"ssl_cert_file=/run/hololive-bot/postgres-tls/server.crt",
		"ssl_key_file=/run/hololive-bot/postgres-tls/server.key",
	} {
		if !strings.Contains(command, flag) {
			t.Fatalf("holo-postgres command in %s missing %q: %q", stackName, flag, command)
		}
	}
}

func assertHoloPostgresTLSMount(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	foundTLSMount := false

	for _, volume := range composeVolumes(t, cfg, serviceHoloPostgres) {
		source := cleanVolumePath(volume.Source)
		target := cleanVolumePath(volume.Target)

		if source == "/etc/stack-secrets/hololive-bot/postgres-tls" && target == "/run/hololive-bot/postgres-tls" {
			if !volume.ReadOnly {
				t.Fatalf("holo-postgres postgres-tls mount must be read-only in %s", stackName)
			}

			foundTLSMount = true
		}

		if target == runtimeCertsDir {
			t.Fatalf("holo-postgres must not mount the shared client certs directory in %s", stackName)
		}
	}

	if !foundTLSMount {
		t.Fatalf("holo-postgres missing /run/hololive-bot/postgres-tls read-only mount in %s", stackName)
	}
}

func assertDBMigrateVerifyFullTLS(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	migrateEnv := composeEnvironment(t, cfg, "hololive-db-migrate")
	if migrateEnv["PGSSLMODE"] != load.PostgresSSLModeVerifyFull {
		t.Fatalf("hololive-db-migrate PGSSLMODE = %q in %s, want verify-full", migrateEnv["PGSSLMODE"], stackName)
	}

	if migrateEnv["PGSSLROOTCERT"] != postgresCACertPath {
		t.Fatalf("hololive-db-migrate PGSSLROOTCERT = %q in %s, want /run/hololive-bot/certs/postgres-ca.pem", migrateEnv["PGSSLROOTCERT"], stackName)
	}

	migrateTargets := strings.Join(composeVolumeTargets(t, cfg, "hololive-db-migrate"), "\n")
	if !strings.Contains(migrateTargets, postgresCACertPath) {
		t.Fatalf("hololive-db-migrate missing postgres-ca.pem mount in %s: %q", stackName, migrateTargets)
	}
}

func TestRepoComposeNoStackRendersWeakPostgresSSLMode(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{
			name:  "live-compat",
			files: []string{composeProdFile, composeLiveCompatFile},
		},
		{
			name: "main-ap live-compat",
			files: []string{
				composeProdFile,
				composeLiveCompatFile,
				"deploy/compose/docker-compose.main-ap.yml",
				"deploy/compose/docker-compose.main-ap.live-compat.yml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderComposeConfig(t, tt.files...)
			for service := range cfg.Services {
				env := composeEnvironment(t, cfg, service)
				if isWeakPostgresSSLMode(env["POSTGRES_SSLMODE"]) {
					t.Fatalf("%s in %s renders weak POSTGRES_SSLMODE=%q; only verify-full is allowed", service, tt.name, env["POSTGRES_SSLMODE"])
				}
			}
		})
	}
}

func TestRepoHololiveComposeUnitExecutesOnlyImmutableRootWrappers_03e6dca8(t *testing.T) {
	unit := readRepoFile(t, "scripts/systemd/hololive-compose.service")

	execDirectives := []string{
		"ExecStart=", "ExecStartPre=", "ExecStartPost=",
		"ExecReload=", "ExecStop=", "ExecStopPost=",
	}

	found := 0

	for line := range strings.SplitSeq(unit, "\n") {
		trimmed := strings.TrimSpace(line)

		for _, directive := range execDirectives {
			if !strings.HasPrefix(trimmed, directive) {
				continue
			}

			found++

			value := strings.TrimPrefix(trimmed, directive)
			binary := systemdExecBinary(value)

			if !strings.HasPrefix(binary, "/usr/local/sbin/") {
				t.Fatalf("%s%s executes %q; a root unit must run only immutable root-owned /usr/local/sbin wrappers, never a kapu-writable repo/home path (privilege escalation 03e6dca8)", directive, value, binary)
			}
		}
	}

	if found == 0 {
		t.Fatal("hololive-compose.service declares no Exec* directives to verify (03e6dca8)")
	}
}

func systemdExecBinary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "-@+!:")

	fields := strings.Fields(value)

	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}

func isWeakPostgresSSLMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disable", "allow", "prefer", "require", "verify-ca":
		return true
	default:
		return false
	}
}

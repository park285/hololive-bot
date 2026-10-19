package settings

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func assertComposePostgresTLS(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	for _, service := range []string{serviceHololiveAPI, serviceAlarmWorker, runtimepolicy.RuntimeYouTubeCollector} {
		env := composeEnvironment(t, cfg, service)
		if env["POSTGRES_SSLMODE"] != runtimepolicy.PostgresSSLModeVerifyFull {
			t.Fatalf("%s in %s POSTGRES_SSLMODE = %q, want verify-full", service, stackName, env["POSTGRES_SSLMODE"])
		}

		if env["POSTGRES_SSLROOTCERT"] != postgresCACertPath {
			t.Fatalf("%s in %s POSTGRES_SSLROOTCERT = %q, want /run/hololive-bot/certs/postgres-ca.pem", service, stackName, env["POSTGRES_SSLROOTCERT"])
		}
	}

	assertHoloPostgresTLSCommand(t, cfg, stackName)
	assertHoloPostgresTLSMount(t, cfg, stackName)
	assertDBMigrateSocketIsolation(t, cfg, stackName)
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

func assertDBMigrateSocketIsolation(t *testing.T, cfg renderedCompose, stackName string) {
	t.Helper()

	const migrator = "hololive-db-migrate"

	service := composeService(t, cfg, migrator)

	if mode := stringValue(service["network_mode"]); mode != "none" {
		t.Fatalf("%s network_mode = %q in %s, want no network access", migrator, mode, stackName)
	}

	if len(composePorts(t, migrator, service)) != 0 {
		t.Fatalf("%s publishes network ports in %s", migrator, stackName)
	}

	env := composeEnvironment(t, cfg, migrator)
	if env["PGHOST"] != "/var/run/postgresql" || env["PGSSLMODE"] != "disable" {
		t.Fatalf("%s must use the local admin socket without TLS in %s", migrator, stackName)
	}

	for name := range cfg.Services {
		mounted := false

		for _, volume := range composeVolumes(t, cfg, name) {
			if volume.Source != "holo-pg-admin-socket" {
				continue
			}

			mounted = true

			if volume.Target != env["PGHOST"] || (name == migrator && !volume.ReadOnly) {
				t.Fatalf("%s has unsafe admin socket mount %+v in %s", name, volume, stackName)
			}
		}

		if mounted != (name == serviceHoloPostgres || name == migrator) {
			t.Fatalf("%s admin socket access = %v in %s", name, mounted, stackName)
		}
	}
}

func assertNoWeakPostgresSSLMode(t *testing.T, cfg renderedCompose) {
	t.Helper()

	for service := range cfg.Services {
		env := composeEnvironment(t, cfg, service)
		if isWeakPostgresSSLMode(env["POSTGRES_SSLMODE"]) {
			t.Fatalf("%s renders weak POSTGRES_SSLMODE=%q; only verify-full is allowed", service, env["POSTGRES_SSLMODE"])
		}
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

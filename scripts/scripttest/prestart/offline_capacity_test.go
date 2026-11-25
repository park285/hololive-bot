package prestart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

type offline struct {
	dir, units, foreign string
	slot                collectorSlot
}

func newOffline(t *testing.T) offline {
	t.Helper()

	dir := t.TempDir()
	o := offline{dir: dir, units: filepath.Join(dir, "units"), foreign: filepath.Join(dir, "foreign"), slot: central(t)}
	(&host{root: dir}).copyGlob(t, "deploy/quadlet/hololive-bot/*.container")
	harness.Write(t, filepath.Join(dir, "offline.env"), "APP_ENV=production\n")
	harness.Write(t, filepath.Join(o.foreign, "hololive-.container.d/60-pool.conf"), "[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
	harness.Write(t, filepath.Join(o.foreign, "hololive-api.service.d/60-pool.conf"), "[Service]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")
	harness.Executable(t, filepath.Join(dir, "bin/systemctl"), "touch \"$MANAGER_CALLED\"\nexit 99\n")

	return o
}

func (o offline) check(t *testing.T, service string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `unset HOLOLIVE_QUADLET_TEST_ROOT
. "$SCRIPTTEST_ROOT/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_assert_service "$SCRIPTTEST_ROOT" "$ENV_FILE" "$UNITS" "$1" "$UNITS/$1.container"
`,
		Args: []string{service},
		Env: []string{
			"ENV_FILE=" + filepath.Join(o.dir, "offline.env"), "UNITS=" + o.units,
			"QUADLET_UNIT_DIRS=" + o.foreign, "SYSTEMD_UNIT_PATH=" + o.foreign,
			"MANAGER_CALLED=" + filepath.Join(o.dir, "manager-called"),
		},
		Bin: filepath.Join(o.dir, "bin"),
	})
}

func (o offline) assertManagerUntouched(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(o.dir, "manager-called")); err == nil {
		t.Fatal("offline capacity check queried the service manager")
	}
}

func TestOfflineCapacityCheckIgnoresHostOriginsButNotItsOwnTree(t *testing.T) {
	t.Parallel()

	o := newOffline(t)
	harness.Succeeds(t, "offline check", o.check(t, apiUnit))
	o.assertManagerUntouched(t)

	harness.Write(t, filepath.Join(o.units, "hololive-.container.d/60-pool.conf"), "[Container]\nEnvironment=ADMIN_API_POSTGRES_POOL_MAX_CONNS=5\n")

	r := o.check(t, apiUnit)
	harness.Fails(t, "inherited override", r)

	if !strings.Contains(r.Stdout+r.Stderr, unsupportedOrigin) {
		t.Fatalf("inherited override:\n%s", r)
	}

	o.assertManagerUntouched(t)
}

func TestOfflineCapacityCheckEnforcesCollectorRoleLimits(t *testing.T) {
	t.Parallel()

	o := newOffline(t)
	service := o.slot.unit
	harness.Succeeds(t, "default collector pool", o.check(t, service))

	dropIn := filepath.Join(o.units, service+".container.d/60-pool.conf")
	cases := map[string]string{
		"8": "aggregate pools exceed role limit: hololive_collector_c",
		"7": "aggregate pools leave no reserved probe connection: hololive_collector_c",
	}

	for limit, want := range cases {
		harness.Write(t, dropIn, "[Container]\nEnvironment=POSTGRES_POOL_MAX_CONNS="+limit+"\n")

		r := o.check(t, service)
		harness.Fails(t, "pool "+limit, r)

		if !strings.Contains(r.Stdout+r.Stderr, want) {
			t.Errorf("pool %s: missing %q:\n%s", limit, want, r)
		}
	}
}

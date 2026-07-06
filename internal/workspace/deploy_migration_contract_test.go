package workspace

import (
	"strings"
	"testing"
)

func TestComposeRedeployRunsMigrationBeforeAppRuntimeCutover(t *testing.T) {
	t.Parallel()

	root := repoRootFromHelper(t)
	script := readRepoFile(t, root, "scripts/deploy/compose-redeploy-service.sh")

	required := []string{
		"target_requires_db_migration",
		"run_db_migration_before_cutover",
		"[MIGRATE] hololive-db-migrate",
		"run --rm hololive-db-migrate",
	}
	for _, token := range required {
		if !strings.Contains(script, token) {
			t.Fatalf("compose redeploy script missing %q", token)
		}
	}

	migrateIndex := strings.Index(script, "run_db_migration_before_cutover")
	upIndex := strings.Index(script, `echo "[UP] ${TARGET}"`)
	if migrateIndex < 0 || upIndex < 0 || migrateIndex > upIndex {
		t.Fatal("compose redeploy script must run migration before target up")
	}
}

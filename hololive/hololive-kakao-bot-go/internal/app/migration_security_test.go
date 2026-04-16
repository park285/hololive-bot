package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestACLMigrationDoesNotSeedRuntimeBehavior(t *testing.T) {
	sql := readMigrationSQL(t, "scripts/migrations/037_acl_blacklist_mode.sql")

	required := []string{
		"ALTER TABLE acl_rooms ADD COLUMN list_type VARCHAR(16) NOT NULL DEFAULT 'whitelist';",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_room_list ON acl_rooms (room_id, list_type);",
	}

	for _, snippet := range required {
		if !strings.Contains(sql, snippet) {
			t.Fatalf("expected ACL migration to contain %q", snippet)
		}
	}

	forbidden := []string{
		"INSERT INTO acl_settings",
		"INSERT INTO acl_rooms",
	}

	for _, snippet := range forbidden {
		if strings.Contains(sql, snippet) {
			t.Fatalf("ACL migration must not seed runtime ACL state: found %q", snippet)
		}
	}
}

func TestSettlementArchiveMigrationDoesNotSeedKakaoUserIdentifiers(t *testing.T) {
	sql := readMigrationSQL(t, "scripts/migrations/archive/settlement/038_create_settlement.sql")

	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS settlement_members") {
		t.Fatal("expected settlement migration to create settlement_members table")
	}

	if strings.Contains(sql, "INSERT INTO settlement_members") {
		t.Fatal("settlement migration must not seed hardcoded Kakao user identifiers")
	}
}

func readMigrationSQL(t *testing.T, relativePath string) string {
	t.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	path := filepath.Join(filepath.Dir(currentFile), "..", "..", relativePath)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", relativePath, err)
	}

	return string(content)
}

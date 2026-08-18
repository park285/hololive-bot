package migrationrunner

import "testing"

func TestEpoch2ManifestContract(t *testing.T) {
	entries := manifestEntries(t)
	if len(entries) == 0 {
		t.Fatal("empty migration manifest")
	}
	if entries[0] != epoch2Baseline {
		t.Fatalf("first migration = %q, want %q", entries[0], epoch2Baseline)
	}

	legacy, err := parseEpoch2LegacyContract(epoch2LegacyContractRaw)
	if err != nil {
		t.Fatalf("parse epoch-2 legacy contract: %v", err)
	}
	for _, migration := range legacy {
		if containsEntry(entries, migration.name) {
			t.Fatalf("legacy migration %q must not remain in epoch-2 manifest", migration.name)
		}
	}
	for _, entry := range entries[1:] {
		if entry < "141_" {
			t.Fatalf("post-baseline migration %q is before retained suffix 141", entry)
		}
	}
}

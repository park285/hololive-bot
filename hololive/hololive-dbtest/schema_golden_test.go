package dbtest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
)

const (
	schemaGoldenDir  = "testdata"
	schemaGoldenFile = "schema_snapshot.golden"
	schemaUpdateEnv  = "SCHEMA_SNAPSHOT_UPDATE"
)

func TestSchemaSnapshotGolden(t *testing.T) {
	t.Parallel()

	pool := NewReplayPool(t)
	ctx := t.Context()

	got, err := SnapshotSchema(ctx, pool)
	if err != nil {
		t.Fatalf("serialize schema: %v", err)
	}

	again, err := SnapshotSchema(ctx, pool)
	if err != nil {
		t.Fatalf("serialize schema (determinism pass): %v", err)
	}

	if got != again {
		t.Fatal("schema serialization is non-deterministic: two dumps of the same database differ")
	}

	if os.Getenv(schemaUpdateEnv) == "1" {
		writeSchemaGolden(t, got)

		return
	}

	wantBytes, err := fs.ReadFile(os.DirFS(schemaGoldenDir), schemaGoldenFile)
	if err != nil {
		t.Fatalf("read golden %s/%s: %v\nregenerate: %s", schemaGoldenDir, schemaGoldenFile, err, schemaRegenCmd)
	}

	want := string(wantBytes)
	if got == want {
		return
	}

	t.Fatalf("schema drift detected: serialized schema differs from committed golden snapshot.\n"+
		"--- unified diff (golden vs current) ---\n%s\n"+
		"if this drift is intentional, regenerate the golden with:\n  %s",
		unifiedSchemaDiff(want, got), schemaRegenCmd)
}

func writeSchemaGolden(t *testing.T, content string) {
	t.Helper()

	if err := os.MkdirAll(schemaGoldenDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", schemaGoldenDir, err)
	}

	path := filepath.Join(schemaGoldenDir, schemaGoldenFile)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write golden %s: %v", path, err)
	}

	t.Logf("updated golden %s (%d bytes)", path, len(content))
}

func unifiedSchemaDiff(want, got string) string {
	out, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(want),
		B:        difflib.SplitLines(got),
		FromFile: schemaGoldenFile,
		ToFile:   "current",
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("(failed to render unified diff: %v)", err)
	}

	return out
}

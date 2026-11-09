// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	schemaGoldenFile = "schema_snapshot.golden.sql"
	schemaUpdateEnv  = "SCHEMA_SNAPSHOT_UPDATE"
)

// pg_dump가 아닌 catalog 직렬화를 쓰는 이유: pg_dump text는 patch/tool version별 헤더·구문
// 차이가 커 결정성을 깬다. 반면 catalog 직렬화는 PG18과 검증된 외부 test DB에서 안정적으로 동작한다.
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

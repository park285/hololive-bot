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
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pmezard/go-difflib/difflib"
)

const (
	schemaGoldenDir  = "testdata"
	schemaGoldenFile = "schema_snapshot.golden.sql"
	schemaUpdateEnv  = "SCHEMA_SNAPSHOT_UPDATE"
	schemaRegenCmd   = "SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-shared/pkg/dbtest"
)

// pg_dump가 아닌 catalog 직렬화를 쓰는 이유: 골든이 primary(pg18)/fallback(pg16) 두 이미지에서
// 동일해야 하는데 pg_dump 출력은 버전별 헤더·구문 차이가 커 결정성을 깬다. catalog 직렬화는
// 버전 무관하게 안정적이고 TEST_DATABASE_URL 외부 DB 경로에서도 Exec 없이 동일하게 동작한다.
func TestSchemaSnapshotGolden(t *testing.T) {
	pool := NewPool(t)
	ctx := context.Background()

	got, err := serializeSchema(ctx, pool)
	if err != nil {
		t.Fatalf("serialize schema: %v", err)
	}

	again, err := serializeSchema(ctx, pool)
	if err != nil {
		t.Fatalf("serialize schema (determinism pass): %v", err)
	}
	if got != again {
		t.Fatalf("schema serialization is non-deterministic: two dumps of the same database differ")
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

func serializeSchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	enums, err := queryEnums(ctx, pool)
	if err != nil {
		return "", err
	}

	tables, err := queryTables(ctx, pool)
	if err != nil {
		return "", err
	}

	columns, err := queryColumns(ctx, pool)
	if err != nil {
		return "", err
	}

	constraints, err := queryConstraints(ctx, pool)
	if err != nil {
		return "", err
	}

	indexes, err := queryIndexes(ctx, pool)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	b.WriteString("-- hololive schema snapshot (deterministic pg_catalog serialization)\n")
	b.WriteString("-- objects: enum types, tables, columns, constraints, indexes\n")
	b.WriteString("-- regenerate: " + schemaRegenCmd + "\n")

	for _, e := range enums {
		b.WriteString("\nENUM " + e.name + "\n")
		for _, label := range e.labels {
			b.WriteString("  " + label + "\n")
		}
	}

	for _, table := range tables {
		b.WriteString("\nTABLE " + table + "\n")

		for _, col := range columns[table] {
			b.WriteString("  COLUMN " + col + "\n")
		}
		for _, con := range constraints[table] {
			b.WriteString("  CONSTRAINT " + con + "\n")
		}
		for _, idx := range indexes[table] {
			b.WriteString("  INDEX " + idx + "\n")
		}
	}

	return b.String(), nil
}

type schemaEnum struct {
	name   string
	labels []string
}

func queryEnums(ctx context.Context, pool *pgxpool.Pool) ([]schemaEnum, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.typname, e.enumlabel
		FROM pg_type t
		JOIN pg_enum e ON e.enumtypid = t.oid
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = current_schema()
		ORDER BY t.typname, e.enumsortorder`)
	if err != nil {
		return nil, fmt.Errorf("query enums: %w", err)
	}
	defer rows.Close()

	byName := map[string][]string{}

	var order []string

	for rows.Next() {
		var name, label string
		if scanErr := rows.Scan(&name, &label); scanErr != nil {
			return nil, fmt.Errorf("scan enum: %w", scanErr)
		}

		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}

		byName[name] = append(byName[name], label)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enums: %w", err)
	}

	enums := make([]schemaEnum, 0, len(order))
	for _, name := range order {
		enums = append(enums, schemaEnum{name: name, labels: byName[name]})
	}

	return enums, nil
}

func queryTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("query tables: %w", err)
	}
	defer rows.Close()

	var tables []string

	for rows.Next() {
		var name string
		if scanErr := rows.Scan(&name); scanErr != nil {
			return nil, fmt.Errorf("scan table: %w", scanErr)
		}

		tables = append(tables, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}

	return tables, nil
}

func queryColumns(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname,
		       a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       a.attnotnull,
		       a.attidentity::text,
		       a.attgenerated::text,
		       pg_get_expr(d.adbin, d.adrelid)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND a.attnum > 0
		  AND NOT a.attisdropped
		ORDER BY c.relname, a.attnum`)
	if err != nil {
		return nil, fmt.Errorf("query columns: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}

	for rows.Next() {
		var table, name, typ, identity, generated string

		var notnull bool

		var def *string

		if scanErr := rows.Scan(&table, &name, &typ, &notnull, &identity, &generated, &def); scanErr != nil {
			return nil, fmt.Errorf("scan column: %w", scanErr)
		}

		out[table] = append(out[table], formatColumn(name, typ, notnull, identity, generated, def))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}

	return out, nil
}

func formatColumn(name, typ string, notnull bool, identity, generated string, def *string) string {
	var b strings.Builder

	b.WriteString(name)
	b.WriteString(" ")
	b.WriteString(typ)

	if notnull {
		b.WriteString(" NOT NULL")
	}

	switch {
	case generated == "s" && def != nil:
		b.WriteString(" GENERATED ALWAYS AS (" + *def + ") STORED")
	case identity == "a":
		b.WriteString(" GENERATED ALWAYS AS IDENTITY")
	case identity == "d":
		b.WriteString(" GENERATED BY DEFAULT AS IDENTITY")
	case def != nil:
		b.WriteString(" DEFAULT " + *def)
	}

	return b.String()
}

func queryConstraints(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT rel.relname,
		       con.conname,
		       pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = rel.relnamespace
		WHERE n.nspname = current_schema()
		  AND con.conrelid <> 0
		  -- contype 'n'은 PG18이 NOT NULL을 catalog에 기록한 것으로 PG16엔 없다. 제외해야 두 이미지 골든이 일치하고, NOT NULL은 COLUMN 라인에 이미 있다.
		  AND con.contype <> 'n'
		ORDER BY rel.relname, con.contype, con.conname`)
	if err != nil {
		return nil, fmt.Errorf("query constraints: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}

	for rows.Next() {
		var table, name, def string
		if scanErr := rows.Scan(&table, &name, &def); scanErr != nil {
			return nil, fmt.Errorf("scan constraint: %w", scanErr)
		}

		out[table] = append(out[table], name+" "+def)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate constraints: %w", err)
	}

	return out, nil
}

// 제약이 소유한 backing index는 CONSTRAINT 섹션에 이미 나타나므로 여기서 제외해 이중 기재를 막는다.
func queryIndexes(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.relname,
		       pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = ic.relnamespace
		WHERE n.nspname = current_schema()
		  AND NOT EXISTS (
		      SELECT 1 FROM pg_constraint c WHERE c.conindid = i.indexrelid
		  )
		ORDER BY t.relname, ic.relname`)
	if err != nil {
		return nil, fmt.Errorf("query indexes: %w", err)
	}
	defer rows.Close()

	out := map[string][]string{}

	for rows.Next() {
		var table, def string
		if scanErr := rows.Scan(&table, &def); scanErr != nil {
			return nil, fmt.Errorf("scan index: %w", scanErr)
		}

		out[table] = append(out[table], def)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexes: %w", err)
	}

	return out, nil
}

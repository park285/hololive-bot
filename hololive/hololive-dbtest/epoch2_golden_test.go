package dbtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/kapu/hololive-shared/pkg/sqlsplit"
)

const (
	epoch2BaselineFile     = "001_schema_epoch2_baseline.sql"
	epoch2CutoffFile       = "139_trust_alarm_short_links.sql"
	epoch2GoldenUpdateEnv  = "EPOCH2_GOLDEN_UPDATE"
	epoch2GoldenSourceEnv  = "EPOCH2_GOLDEN_SOURCE_DIR"
	epoch2SchemaGoldenFile = "epoch2_cutoff_schema.golden.sql"
	epoch2DataGoldenFile   = "epoch2_cutoff_data.golden.json"
	epoch2ACLGoldenFile    = "epoch2_cutoff_acl.golden.txt"
)

type epoch2Roles struct {
	scraper string
	runtime string
}

type epoch2DataSnapshot struct {
	Tables                   map[string][]json.RawMessage `json:"tables"`
	Sequences                map[string]epoch2Sequence    `json:"sequences"`
	VolatileTimestampColumns map[string][]string          `json:"volatile_timestamp_columns"`
}

type epoch2Sequence struct {
	LastValue string `json:"last_value"`
	IsCalled  bool   `json:"is_called"`
}

func TestEpoch2CutoffGoldens(t *testing.T) {
	pool := NewBlankPool(t)
	roles := createEpoch2Roles(t, pool)
	applyEpoch2Cutoff(t, pool, roles)

	schema, err := serializeEpoch2Schema(t.Context(), pool)
	if err != nil {
		t.Fatalf("serialize epoch-2 schema: %v", err)
	}
	data, err := serializeEpoch2Data(t.Context(), pool)
	if err != nil {
		t.Fatalf("serialize epoch-2 data: %v", err)
	}
	acl, err := serializeEpoch2ACL(t.Context(), pool, roles)
	if err != nil {
		t.Fatalf("serialize epoch-2 ACL: %v", err)
	}

	assertEpoch2Deterministic(t, pool, roles, schema, data, acl)
	assertOrUpdateEpoch2Golden(t, epoch2SchemaGoldenFile, schema)
	assertOrUpdateEpoch2Golden(t, epoch2DataGoldenFile, data)
	assertOrUpdateEpoch2Golden(t, epoch2ACLGoldenFile, acl)
}

func createEpoch2Roles(t *testing.T, pool *pgxpool.Pool) epoch2Roles {
	t.Helper()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	roles := epoch2Roles{
		scraper: "epoch2_scraper_" + suffix,
		runtime: "epoch2_runtime_" + suffix,
	}

	ctx := t.Context()
	for _, role := range []string{roles.scraper, roles.runtime} {
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := pool.Exec(ctx, "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
			t.Fatalf("create epoch-2 role %s: %v", role, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, role := range []string{roles.scraper, roles.runtime} {
			quoted := pgx.Identifier{role}.Sanitize()
			if _, err := pool.Exec(cleanupCtx, "DROP OWNED BY "+quoted); err != nil {
				t.Errorf("drop epoch-2 role ownership %s: %v", role, err)
				continue
			}
			if _, err := pool.Exec(cleanupCtx, "DROP ROLE "+quoted); err != nil {
				t.Errorf("drop epoch-2 role %s: %v", role, err)
			}
		}
	})

	scraper := pgx.Identifier{roles.scraper}.Sanitize()
	runtime := pgx.Identifier{roles.runtime}.Sanitize()
	statements := []string{
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
		"GRANT USAGE ON SCHEMA public TO " + scraper + ", " + runtime,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM PUBLIC",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + runtime,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + runtime,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO " + runtime,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("configure epoch-2 ACL fixture: %v", err)
		}
	}
	return roles
}

func applyEpoch2Cutoff(t *testing.T, pool *pgxpool.Pool, roles epoch2Roles) {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv(epoch2GoldenSourceEnv))
	if dir == "" {
		var err error
		dir, err = resolveMigrationsDir()
		if err != nil {
			t.Fatalf("resolve epoch-2 migrations: %v", err)
		}
		applyEpoch2Migration(t, pool, filepath.Join(dir, epoch2BaselineFile), roles)
		return
	}

	entries, err := readManifest(filepath.Join(dir, manifestFileName))
	if err != nil {
		t.Fatalf("read epoch-2 source manifest: %v", err)
	}
	foundCutoff := false
	for _, name := range entries {
		applyEpoch2Migration(t, pool, filepath.Join(dir, name), roles)
		if name == epoch2CutoffFile {
			foundCutoff = true
			break
		}
	}
	if !foundCutoff {
		t.Fatalf("epoch-2 source manifest does not contain cutoff %s", epoch2CutoffFile)
	}
}

func applyEpoch2Migration(t *testing.T, pool *pgxpool.Pool, path string, roles epoch2Roles) {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS(filepath.Dir(path)), filepath.Base(path))
	if err != nil {
		t.Fatalf("read epoch-2 migration %s: %v", filepath.Base(path), err)
	}
	content := strings.NewReplacer(
		"hololive_scraper", roles.scraper,
		"hololive_runtime", roles.runtime,
	).Replace(string(raw))
	segments, err := sqlsplit.Segments(content)
	if err != nil {
		t.Fatalf("split epoch-2 migration %s: %v", filepath.Base(path), err)
	}
	for _, segment := range segments {
		if err := applyMigrationSegment(t.Context(), pool, filepath.Base(path), segment); err != nil {
			t.Fatalf("apply epoch-2 migration %s: %v", filepath.Base(path), err)
		}
	}
}

func serializeEpoch2Schema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	snapshot, err := querySchema(ctx, pool)
	if err != nil {
		return "", err
	}
	body := snapshot.serialize()
	separator := strings.Index(body, "\n\n")
	if separator < 0 {
		return "", fmt.Errorf("schema serialization header is malformed")
	}
	return "-- holobot epoch-2 cutoff schema\n-- source: legacy migrations through 139_trust_alarm_short_links.sql\n" + body[separator:], nil
}

func serializeEpoch2Data(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	tables, volatileColumns, err := snapshotEpoch2Tables(ctx, pool)
	if err != nil {
		return "", err
	}
	sequences, err := snapshotEpoch2Sequences(ctx, pool)
	if err != nil {
		return "", err
	}
	snapshot := epoch2DataSnapshot{
		Tables:                   tables,
		Sequences:                sequences,
		VolatileTimestampColumns: volatileColumns,
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal epoch-2 data: %w", err)
	}
	return string(raw) + "\n", nil
}

func snapshotEpoch2Tables(ctx context.Context, pool *pgxpool.Pool) (tables map[string][]json.RawMessage, volatileColumns map[string][]string, resultErr error) {
	tableNames, err := queryTables(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	volatileColumns, err = queryEpoch2VolatileTimestampColumns(ctx, pool)
	if err != nil {
		return nil, nil, err
	}
	tables = make(map[string][]json.RawMessage, len(tableNames))
	for _, table := range tableNames {
		values, err := queryEpoch2TableRows(ctx, pool, table, volatileColumns[table])
		if err != nil {
			return nil, nil, err
		}
		tables[table] = values
	}
	return tables, volatileColumns, nil
}

func queryEpoch2TableRows(ctx context.Context, pool *pgxpool.Pool, table string, volatileColumns []string) ([]json.RawMessage, error) {
	rows, err := pool.Query(ctx, "SELECT to_jsonb(t)::text FROM "+pgx.Identifier{"public", table}.Sanitize()+" AS t")
	if err != nil {
		return nil, fmt.Errorf("query data table %s: %w", table, err)
	}
	defer rows.Close()
	values := make([]json.RawMessage, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan data table %s: %w", table, err)
		}
		normalized, err := normalizeEpoch2DataRow(value, volatileColumns)
		if err != nil {
			return nil, fmt.Errorf("normalize data table %s: %w", table, err)
		}
		values = append(values, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate data table %s: %w", table, err)
	}
	sort.Slice(values, func(i, j int) bool { return string(values[i]) < string(values[j]) })
	return values, nil
}

func snapshotEpoch2Sequences(ctx context.Context, pool *pgxpool.Pool) (map[string]epoch2Sequence, error) {
	names, err := queryEpoch2SequenceNames(ctx, pool)
	if err != nil {
		return nil, err
	}
	sequences := make(map[string]epoch2Sequence, len(names))
	for _, name := range names {
		var state epoch2Sequence
		if err := pool.QueryRow(ctx, "SELECT last_value::text, is_called FROM "+pgx.Identifier{"public", name}.Sanitize()).Scan(&state.LastValue, &state.IsCalled); err != nil {
			return nil, fmt.Errorf("query data sequence %s: %w", name, err)
		}
		sequences[name] = state
	}
	return sequences, nil
}

func queryEpoch2SequenceNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'S'
		ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("query data sequences: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan data sequence: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate data sequences: %w", err)
	}
	return names, nil
}

func queryEpoch2VolatileTimestampColumns(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname, a.attname
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		  AND a.atttypid IN ('timestamp'::regtype, 'timestamptz'::regtype)
		  AND pg_get_expr(d.adbin, d.adrelid) ~* '(now\(\)|CURRENT_TIMESTAMP|clock_timestamp\(\))'
		ORDER BY c.relname, a.attname`)
	if err != nil {
		return nil, fmt.Errorf("query volatile timestamp columns: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]string)
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, fmt.Errorf("scan volatile timestamp column: %w", err)
		}
		out[table] = append(out[table], column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate volatile timestamp columns: %w", err)
	}
	return out, nil
}

func normalizeEpoch2DataRow(value string, volatileColumns []string) (json.RawMessage, error) {
	row := make(map[string]json.RawMessage)
	if err := json.Unmarshal([]byte(value), &row); err != nil {
		return nil, err
	}
	for _, column := range volatileColumns {
		if raw, exists := row[column]; exists && string(raw) != "null" {
			row[column] = json.RawMessage(`"<migration-time>"`)
		}
	}
	normalized, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(normalized), nil
}

func serializeEpoch2ACL(ctx context.Context, pool *pgxpool.Pool, roles epoch2Roles) (string, error) {
	rows, err := pool.Query(ctx, `
		WITH principals AS (
			SELECT oid, rolname FROM pg_roles WHERE rolname = ANY($1::text[])
			UNION ALL
			SELECT 0::oid, 'PUBLIC'
		), privileges AS (
			SELECT CASE WHEN c.relkind = 'S' THEN 'SEQUENCE' ELSE 'TABLE' END AS object_type,
			       n.nspname || '.' || c.relname AS object_name,
			       p.rolname AS grantee,
			       a.privilege_type,
			       a.is_grantable
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			CROSS JOIN LATERAL aclexplode(COALESCE(c.relacl, acldefault(CASE WHEN c.relkind = 'S' THEN 'S'::"char" ELSE 'r'::"char" END, c.relowner))) a
			JOIN principals p ON p.oid = a.grantee
			WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p', 'S')
			UNION ALL
			SELECT 'FUNCTION', n.nspname || '.' || pfn.proname || '(' || pg_get_function_identity_arguments(pfn.oid) || ')',
			       p.rolname, a.privilege_type, a.is_grantable
			FROM pg_proc pfn
			JOIN pg_namespace n ON n.oid = pfn.pronamespace
			CROSS JOIN LATERAL aclexplode(COALESCE(pfn.proacl, acldefault('f', pfn.proowner))) a
			JOIN principals p ON p.oid = a.grantee
			WHERE n.nspname = current_schema()
			UNION ALL
			SELECT 'SCHEMA', n.nspname, p.rolname, a.privilege_type, a.is_grantable
			FROM pg_namespace n
			CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) a
			JOIN principals p ON p.oid = a.grantee
			WHERE n.nspname = current_schema()
		)
		SELECT object_type, object_name, grantee, privilege_type, is_grantable
		FROM privileges
		ORDER BY object_type, object_name, grantee, privilege_type, is_grantable`, []string{roles.scraper, roles.runtime})
	if err != nil {
		return "", fmt.Errorf("query epoch-2 ACL: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var objectType, objectName, grantee, privilege string
		var grantable bool
		if err := rows.Scan(&objectType, &objectName, &grantee, &privilege, &grantable); err != nil {
			return "", fmt.Errorf("scan epoch-2 ACL: %w", err)
		}
		switch grantee {
		case roles.scraper:
			grantee = "hololive_scraper"
		case roles.runtime:
			grantee = "hololive_runtime"
		}
		lines = append(lines, fmt.Sprintf("%s %s GRANTEE %s PRIVILEGE %s GRANTABLE %t", objectType, objectName, grantee, privilege, grantable))
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate epoch-2 ACL: %w", err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n", nil
}

func assertEpoch2Deterministic(t *testing.T, pool *pgxpool.Pool, roles epoch2Roles, schema, data, acl string) {
	t.Helper()
	againSchema, err := serializeEpoch2Schema(t.Context(), pool)
	if err != nil {
		t.Fatalf("serialize epoch-2 schema again: %v", err)
	}
	againData, err := serializeEpoch2Data(t.Context(), pool)
	if err != nil {
		t.Fatalf("serialize epoch-2 data again: %v", err)
	}
	againACL, err := serializeEpoch2ACL(t.Context(), pool, roles)
	if err != nil {
		t.Fatalf("serialize epoch-2 ACL again: %v", err)
	}
	if schema != againSchema || data != againData || acl != againACL {
		t.Fatal("epoch-2 cutoff serialization is non-deterministic")
	}
}

func assertOrUpdateEpoch2Golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(schemaGoldenDir, name)
	if os.Getenv(epoch2GoldenUpdateEnv) == "1" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write epoch-2 golden %s: %v", path, err)
		}
		t.Logf("updated epoch-2 golden %s (%d bytes)", path, len(got))
		return
	}
	want, err := fs.ReadFile(os.DirFS(schemaGoldenDir), name)
	if err != nil {
		t.Fatalf("read epoch-2 golden %s: %v", path, err)
	}
	if string(want) == got {
		return
	}
	diff, diffErr := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(string(want)), B: difflib.SplitLines(got),
		FromFile: name, ToFile: "current", Context: 3,
	})
	if diffErr != nil {
		t.Fatalf("epoch-2 golden %s differs (render diff: %v)", name, diffErr)
	}
	t.Fatalf("epoch-2 golden %s differs:\n%s", name, diff)
}

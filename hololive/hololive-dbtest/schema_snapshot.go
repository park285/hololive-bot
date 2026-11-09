package dbtest

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaRegenCmd = "SCHEMA_SNAPSHOT_UPDATE=1 go test -run TestSchemaSnapshotGolden ./hololive/hololive-dbtest"

// SnapshotSchema는 fresh bootstrap과 원장 전환의 스키마를 같은 catalog 표현으로 비교합니다.
func SnapshotSchema(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	schema, err := querySchema(ctx, pool)
	if err != nil {
		return "", fmt.Errorf("query schema: %w", err)
	}

	return schema.serialize(), nil
}

type schemaSnapshot struct {
	enums        []schemaEnum
	tables       []string
	columns      map[string][]string
	constraints  map[string][]string
	indexes      map[string][]string
	tableOptions map[string]string
	triggers     map[string][]string
	sequences    []string
	functions    []string
}

func querySchema(ctx context.Context, pool *pgxpool.Pool) (schemaSnapshot, error) {
	var (
		schema schemaSnapshot
		err    error
	)

	schema.enums, err = queryEnums(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query enums: %w", err)
	}

	schema.tables, err = queryTables(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query tables: %w", err)
	}

	schema.columns, err = queryColumns(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query columns: %w", err)
	}

	schema.constraints, err = queryConstraints(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query constraints: %w", err)
	}

	schema.indexes, err = queryIndexes(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query indexes: %w", err)
	}

	schema.tableOptions, err = queryTableOptions(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query table options: %w", err)
	}

	schema.triggers, err = queryTriggers(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query triggers: %w", err)
	}

	schema.sequences, err = querySequences(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query sequences: %w", err)
	}

	schema.functions, err = queryFunctions(ctx, pool)
	if err != nil {
		return schemaSnapshot{}, fmt.Errorf("query functions: %w", err)
	}

	return schema, nil
}

func (schema *schemaSnapshot) serialize() string {
	var b strings.Builder

	b.WriteString("-- hololive schema snapshot (deterministic pg_catalog serialization)\n")
	b.WriteString("-- objects: enum types, tables, columns, constraints, indexes, reloptions, triggers, sequences, functions\n")
	b.WriteString("-- regenerate: " + schemaRegenCmd + "\n")

	for _, e := range schema.enums {
		b.WriteString("\nENUM " + e.name + "\n")

		for _, label := range e.labels {
			b.WriteString("  " + label + "\n")
		}
	}

	for _, table := range schema.tables {
		b.WriteString("\nTABLE " + table + "\n")

		if options := schema.tableOptions[table]; options != "" {
			b.WriteString("  OPTIONS " + options + "\n")
		}

		for _, col := range schema.columns[table] {
			b.WriteString("  COLUMN " + col + "\n")
		}

		for _, con := range schema.constraints[table] {
			b.WriteString("  CONSTRAINT " + con + "\n")
		}

		for _, idx := range schema.indexes[table] {
			b.WriteString("  INDEX " + idx + "\n")
		}

		for _, trigger := range schema.triggers[table] {
			b.WriteString("  TRIGGER " + trigger + "\n")
		}
	}

	for _, sequence := range schema.sequences {
		b.WriteString("\nSEQUENCE " + sequence + "\n")
	}

	for _, function := range schema.functions {
		b.WriteString("\nFUNCTION " + function + "\n")
	}

	return b.String()
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
		  -- contype 'n'은 PG18의 NOT NULL catalog 표현이며 COLUMN 라인에 이미 있으므로 중복을 제외한다.
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

func queryTableOptions(ctx context.Context, pool *pgxpool.Pool) (map[string]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname,
		       COALESCE((SELECT string_agg(option, ',' ORDER BY option) FROM unnest(c.reloptions) option), '')
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relkind IN ('r', 'p')
		ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("query table options: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)

	for rows.Next() {
		var table, options string

		if err := rows.Scan(&table, &options); err != nil {
			return nil, fmt.Errorf("scan table options: %w", err)
		}

		out[table] = options
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table options: %w", err)
	}

	return out, nil
}

func queryTriggers(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.relname, pg_get_triggerdef(t.oid, true)
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND NOT t.tgisinternal
		ORDER BY c.relname, t.tgname`)
	if err != nil {
		return nil, fmt.Errorf("query triggers: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]string)

	for rows.Next() {
		var table, definition string

		if err := rows.Scan(&table, &definition); err != nil {
			return nil, fmt.Errorf("scan trigger: %w", err)
		}

		out[table] = append(out[table], definition)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate triggers: %w", err)
	}

	return out, nil
}

func querySequences(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT seq.relname,
		       format_type(s.seqtypid, NULL),
		       s.seqstart,
		       s.seqincrement,
		       s.seqmin,
		       s.seqmax,
		       s.seqcache,
		       s.seqcycle,
		       COALESCE(owner.relname || '.' || attr.attname, '')
		FROM pg_class seq
		JOIN pg_namespace n ON n.oid = seq.relnamespace
		JOIN pg_sequence s ON s.seqrelid = seq.oid
		LEFT JOIN pg_depend dep
		  ON dep.classid = 'pg_class'::regclass
		 AND dep.objid = seq.oid
		 AND dep.refclassid = 'pg_class'::regclass
		 AND dep.deptype IN ('a', 'i')
		LEFT JOIN pg_class owner ON owner.oid = dep.refobjid
		LEFT JOIN pg_attribute attr ON attr.attrelid = dep.refobjid AND attr.attnum = dep.refobjsubid
		WHERE n.nspname = current_schema()
		ORDER BY seq.relname`)
	if err != nil {
		return nil, fmt.Errorf("query sequences: %w", err)
	}
	defer rows.Close()

	var out []string

	for rows.Next() {
		var (
			name, typ, ownedBy                          string
			start, increment, minValue, maxValue, cache int64
			cycle                                       bool
		)

		if err := rows.Scan(&name, &typ, &start, &increment, &minValue, &maxValue, &cache, &cycle, &ownedBy); err != nil {
			return nil, fmt.Errorf("scan sequence: %w", err)
		}

		definition := fmt.Sprintf("%s AS %s START %d INCREMENT %d MIN %d MAX %d CACHE %d CYCLE %t", name, typ, start, increment, minValue, maxValue, cache, cycle)

		if ownedBy != "" {
			definition += " OWNED BY " + ownedBy
		}

		out = append(out, definition)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sequences: %w", err)
	}

	return out, nil
}

func queryFunctions(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT p.proname,
		       pg_get_function_identity_arguments(p.oid),
		       pg_get_function_result(p.oid),
		       l.lanname,
		       p.provolatile::text,
		       p.prosecdef,
		       p.proleakproof,
		       p.proparallel::text,
		       COALESCE(array_to_string(p.proconfig, ','), ''),
		       p.prosrc
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname = current_schema()
		ORDER BY p.proname, pg_get_function_identity_arguments(p.oid)`)
	if err != nil {
		return nil, fmt.Errorf("query functions: %w", err)
	}
	defer rows.Close()

	var out []string

	for rows.Next() {
		var (
			name, args, result, language, volatility, parallel, config, source string
			securityDefiner, leakproof                                         bool
		)

		if err := rows.Scan(&name, &args, &result, &language, &volatility, &securityDefiner, &leakproof, &parallel, &config, &source); err != nil {
			return nil, fmt.Errorf("scan function: %w", err)
		}

		definition := fmt.Sprintf("%s(%s) RETURNS %s LANGUAGE %s VOLATILITY %s SECURITY_DEFINER %t LEAKPROOF %t PARALLEL %s", name, args, result, language, volatility, securityDefiner, leakproof, parallel)

		if config != "" {
			definition += " CONFIG " + config
		}

		definition += fmt.Sprintf(" BODY %q", source)

		out = append(out, definition)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate functions: %w", err)
	}

	return out, nil
}

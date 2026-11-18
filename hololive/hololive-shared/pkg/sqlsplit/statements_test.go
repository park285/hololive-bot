package sqlsplit

import (
	"reflect"
	"testing"
)

const (
	stmtSelect1 = "SELECT 1"
	stmtSelect2 = "SELECT 2"
)

func TestStatements(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "single statement no trailing semicolon",
			in:   "CREATE INDEX CONCURRENTLY foo ON t (c)",
			want: []string{"CREATE INDEX CONCURRENTLY foo ON t (c)"},
		},
		{
			name: "two statements split on top-level semicolon",
			in:   "CREATE INDEX CONCURRENTLY foo ON t (c);\nCOMMENT ON INDEX foo IS 'x';",
			want: []string{"CREATE INDEX CONCURRENTLY foo ON t (c)", "COMMENT ON INDEX foo IS 'x'"},
		},
		{
			name: "semicolon inside single-quoted literal is not a separator",
			in:   "INSERT INTO t(b) VALUES ('a; b; c'); SELECT 1;",
			want: []string{"INSERT INTO t(b) VALUES ('a; b; c')", stmtSelect1},
		},
		{
			name: "escaped single quote inside literal",
			in:   "INSERT INTO t(b) VALUES ('it''s; fine'); SELECT 2;",
			want: []string{"INSERT INTO t(b) VALUES ('it''s; fine')", stmtSelect2},
		},
		{
			name: "dollar-quoted DO block keeps inner semicolons",
			in: `DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'x') THEN
        CREATE TYPE x AS ENUM ('A', 'B');
    END IF;
END $$;
SELECT 3;`,
			want: []string{
				"DO $$\nBEGIN\n    IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'x') THEN\n        CREATE TYPE x AS ENUM ('A', 'B');\n    END IF;\nEND $$",
				"SELECT 3",
			},
		},
		{
			name: "line comment with semicolon is preserved and ignored as separator",
			in:   "SELECT 1; -- trailing; comment\nSELECT 2;",
			want: []string{stmtSelect1, "-- trailing; comment\nSELECT 2"},
		},
		{
			name: "block comment with semicolon is preserved",
			in:   "SELECT 1 /* a; b */ ; SELECT 2;",
			want: []string{"SELECT 1 /* a; b */", stmtSelect2},
		},
		{
			name: "nested block comment keeps semicolon after inner close",
			in:   "SELECT 1 /* outer /* inner */ still; outer */; SELECT 2;",
			want: []string{"SELECT 1 /* outer /* inner */ still; outer */", stmtSelect2},
		},
		{
			name: "escape string keeps escaped quote and semicolon",
			in:   `SELECT E'a\';b'; SELECT 5;`,
			want: []string{`SELECT E'a\';b'`, "SELECT 5"},
		},
		{
			name: "trailing whitespace and empty fragments dropped",
			in:   "SELECT 1;;  \n ; SELECT 2; \n",
			want: []string{stmtSelect1, stmtSelect2},
		},
		{
			name: "double-quoted identifier with semicolon",
			in:   `CREATE TABLE "weird;name" (id int); SELECT 4;`,
			want: []string{`CREATE TABLE "weird;name" (id int)`, "SELECT 4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Statements(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Statements mismatch\n in:   %q\n got:  %#v\n want: %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "block comment between keywords becomes a space",
			in:   "DROP/* x */INDEX foo",
			want: "DROP INDEX foo",
		},
		{
			name: "line comment keeps its newline",
			in:   "CREATE -- note\nINDEX foo ON t (c)",
			want: "CREATE \nINDEX foo ON t (c)",
		},
		{
			name: "nested block comment is removed as one comment",
			in:   "CREATE /* a /* b */ c */ INDEX CONCURRENTLY foo ON t (c)",
			want: "CREATE   INDEX CONCURRENTLY foo ON t (c)",
		},
		{
			name: "comment markers inside dollar quote are kept",
			in:   "DO $body$ BEGIN -- keep\n/* keep */ END $body$; -- drop",
			want: "DO $body$ BEGIN -- keep\n/* keep */ END $body$; ",
		},
		{
			name: "escape string keeps escaped quote before comment marker",
			in:   `SELECT E'a\'--b' -- drop`,
			want: `SELECT E'a\'--b' `,
		},
		{
			name: "standard string treats backslash as a literal character",
			in:   `SELECT 'a\' -- drop`,
			want: `SELECT 'a\' `,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripComments(tt.in); got != tt.want {
				t.Fatalf("StripComments mismatch\n in:   %q\n got:  %q\n want: %q", tt.in, got, tt.want)
			}
		})
	}
}

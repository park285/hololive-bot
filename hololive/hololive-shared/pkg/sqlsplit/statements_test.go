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

package sqlsplit

import (
	"reflect"
	"testing"
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
			want: []string{"INSERT INTO t(b) VALUES ('a; b; c')", "SELECT 1"},
		},
		{
			name: "escaped single quote inside literal",
			in:   "INSERT INTO t(b) VALUES ('it''s; fine'); SELECT 2;",
			want: []string{"INSERT INTO t(b) VALUES ('it''s; fine')", "SELECT 2"},
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
			want: []string{"SELECT 1", "-- trailing; comment\nSELECT 2"},
		},
		{
			name: "block comment with semicolon is preserved",
			in:   "SELECT 1 /* a; b */ ; SELECT 2;",
			want: []string{"SELECT 1 /* a; b */", "SELECT 2"},
		},
		{
			name: "nested block comment keeps semicolon after inner close",
			in:   "SELECT 1 /* outer /* inner */ still; outer */; SELECT 2;",
			want: []string{"SELECT 1 /* outer /* inner */ still; outer */", "SELECT 2"},
		},
		{
			name: "escape string keeps escaped quote and semicolon",
			in:   `SELECT E'a\';b'; SELECT 5;`,
			want: []string{`SELECT E'a\';b'`, "SELECT 5"},
		},
		{
			name: "trailing whitespace and empty fragments dropped",
			in:   "SELECT 1;;  \n ; SELECT 2; \n",
			want: []string{"SELECT 1", "SELECT 2"},
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

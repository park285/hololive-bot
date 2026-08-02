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

package dbx

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPostgresPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "no placeholders", in: "SELECT 1", want: "SELECT 1"},
		{name: "two placeholders", in: "?,?", want: "$1,$2"},
		{name: "spaced placeholders", in: "a = ? AND b = ?", want: "a = $1 AND b = $2"},
		{name: "three placeholders in clause", in: "x IN (?, ?, ?)", want: "x IN ($1, $2, $3)"},
		{name: "in clause built from InPlaceholders", in: "x IN (" + InPlaceholders(2) + ")", want: "x IN ($1, $2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresPlaceholders(tt.in); got != tt.want {
				t.Errorf("PostgresPlaceholders(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPostgresPlaceholdersRewritesNonPlaceholderQuestionMarks(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "string literal", in: "WHERE note = '?'", want: "WHERE note = '$1'"},
		{name: "jsonb exists operator", in: "WHERE payload ? 'key'", want: "WHERE payload $1 'key'"},
		{name: "jsonb any operator", in: "WHERE payload ?| $1", want: "WHERE payload $1| $1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresPlaceholders(tt.in); got != tt.want {
				t.Errorf("PostgresPlaceholders(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEmbeddedSQLAssetsHaveNoNonPlaceholderQuestionMarks(t *testing.T) {
	moduleRoot := filepath.Join("..", "..")

	err := filepath.Walk(moduleRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".sql") || filepath.Base(filepath.Dir(path)) != "queries" {
			return nil
		}

		data, readErr := os.ReadFile(path) //nolint:gosec // walk 결과 경로만 읽는다.
		if readErr != nil {
			return readErr
		}
		for _, hazard := range questionMarkHazards(string(data)) {
			t.Errorf("%s: %s: PostgresPlaceholders would rewrite this '?'", path, hazard)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk SQL assets: %v", err)
	}
}

func questionMarkHazards(sql string) []string {
	var hazards []string
	quoted := false
	for i := range len(sql) {
		switch {
		case sql[i] == '\'':
			quoted = !quoted
		case sql[i] != '?':
		case quoted:
			hazards = append(hazards, "quoted literal at offset "+strconv.Itoa(i))
		case i+1 < len(sql) && (sql[i+1] == '|' || sql[i+1] == '&'):
			hazards = append(hazards, "jsonb operator at offset "+strconv.Itoa(i))
		}
	}
	return hazards
}

func TestInPlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  string
	}{
		{name: "zero", count: 0, want: ""},
		{name: "negative", count: -1, want: ""},
		{name: "one", count: 1, want: "?"},
		{name: "three", count: 3, want: "?, ?, ?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InPlaceholders(tt.count); got != tt.want {
				t.Errorf("InPlaceholders(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestAnyArgs(t *testing.T) {
	got := AnyArgs([]int64{1, 2, 3})
	if len(got) != 3 {
		t.Fatalf("AnyArgs len = %d, want 3", len(got))
	}
	for i, want := range []int64{1, 2, 3} {
		v, ok := got[i].(int64)
		if !ok || v != want {
			t.Errorf("AnyArgs[%d] = %v, want %d", i, got[i], want)
		}
	}

	if got := AnyArgs([]string{}); len(got) != 0 {
		t.Errorf("AnyArgs(empty) len = %d, want 0", len(got))
	}
}

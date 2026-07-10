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
	"strings"
	"testing"
)

func TestSegments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Segment
	}{
		{
			name: "plain statements form one autocommit segment",
			in:   "CREATE TABLE a(id int);\nCREATE TABLE b(id int);",
			want: []Segment{
				{Transactional: false, Statements: []string{"CREATE TABLE a(id int)", "CREATE TABLE b(id int)"}},
			},
		},
		{
			name: "begin commit block strips tokens and marks segment transactional",
			in:   "BEGIN;\nCREATE TABLE a(id int);\nINSERT INTO a VALUES (1);\nCOMMIT;",
			want: []Segment{
				{Transactional: true, Statements: []string{"CREATE TABLE a(id int)", "INSERT INTO a VALUES (1)"}},
			},
		},
		{
			name: "trailing statement after COMMIT runs autocommit (017 pattern)",
			in:   "BEGIN;\nCREATE TABLE a(id int);\nCOMMIT;\nCOMMENT ON TABLE a IS 'x';",
			want: []Segment{
				{Transactional: true, Statements: []string{"CREATE TABLE a(id int)"}},
				{Transactional: false, Statements: []string{"COMMENT ON TABLE a IS 'x'"}},
			},
		},
		{
			name: "leading comments before BEGIN do not hide the token",
			in:   "-- header\n/* block /* nested */ note */\nBEGIN;\nSELECT 1;\nCOMMIT;",
			want: []Segment{
				{Transactional: true, Statements: []string{"SELECT 1"}},
			},
		},
		{
			name: "lowercase and keyword-tail forms are recognized",
			in:   "begin transaction;\nSELECT 1;\ncommit work;",
			want: []Segment{
				{Transactional: true, Statements: []string{"SELECT 1"}},
			},
		},
		{
			name: "START TRANSACTION and END are synonyms",
			in:   "START TRANSACTION;\nSELECT 1;\nEND;",
			want: []Segment{
				{Transactional: true, Statements: []string{"SELECT 1"}},
			},
		},
		{
			name: "empty transaction block yields no segment",
			in:   "BEGIN;\nCOMMIT;\nSELECT 1;",
			want: []Segment{
				{Transactional: false, Statements: []string{"SELECT 1"}},
			},
		},
		{
			name: "multiple blocks alternate with autocommit statements",
			in:   "SELECT 0;\nBEGIN;\nSELECT 1;\nCOMMIT;\nBEGIN;\nSELECT 2;\nCOMMIT;",
			want: []Segment{
				{Transactional: false, Statements: []string{"SELECT 0"}},
				{Transactional: true, Statements: []string{"SELECT 1"}},
				{Transactional: true, Statements: []string{"SELECT 2"}},
			},
		},
		{
			name: "BEGIN inside dollar-quoted DO body is not transaction control",
			in:   "DO $$\nBEGIN\n    PERFORM 1;\nEND $$;",
			want: []Segment{
				{Transactional: false, Statements: []string{"DO $$\nBEGIN\n    PERFORM 1;\nEND $$"}},
			},
		},
		{
			name: "CONCURRENTLY in literals and comments is not a transaction keyword",
			in:   "BEGIN;\nSELECT 'CONCURRENTLY';\n/* CONCURRENTLY */ SELECT 1;\nDO $$ BEGIN RAISE NOTICE 'CONCURRENTLY'; END $$;\nCOMMIT;",
			want: []Segment{
				{Transactional: true, Statements: []string{
					"SELECT 'CONCURRENTLY'",
					"/* CONCURRENTLY */ SELECT 1",
					"DO $$ BEGIN RAISE NOTICE 'CONCURRENTLY'; END $$",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Segments(tt.in)
			if err != nil {
				t.Fatalf("Segments() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Segments mismatch\n in:   %q\n got:  %#v\n want: %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSegmentsErrors(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{
			name:    "missing COMMIT",
			in:      "BEGIN;\nSELECT 1;",
			wantErr: "without a matching COMMIT",
		},
		{
			name:    "COMMIT without BEGIN",
			in:      "SELECT 1;\nCOMMIT;",
			wantErr: "COMMIT without a matching top-level BEGIN",
		},
		{
			name:    "nested BEGIN",
			in:      "BEGIN;\nBEGIN;\nSELECT 1;\nCOMMIT;",
			wantErr: "nested BEGIN",
		},
		{
			name:    "CONCURRENTLY inside block",
			in:      "BEGIN;\nCREATE INDEX CONCURRENTLY i ON t(c);\nCOMMIT;",
			wantErr: "CONCURRENTLY",
		},
		{
			name:    "ROLLBACK is not replayable",
			in:      "BEGIN;\nSELECT 1;\nROLLBACK;",
			wantErr: "unsupported top-level transaction control",
		},
		{
			name:    "SAVEPOINT is not replayable",
			in:      "BEGIN;\nSAVEPOINT sp1;\nCOMMIT;",
			wantErr: "unsupported top-level transaction control",
		},
		{
			name:    "BEGIN with isolation modes is not replayable",
			in:      "BEGIN ISOLATION LEVEL SERIALIZABLE;\nSELECT 1;\nCOMMIT;",
			wantErr: "unsupported transaction control statement",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Segments(tt.in)
			if err == nil {
				t.Fatalf("Segments(%q) error = nil, want %q", tt.in, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Segments(%q) error = %v, want substring %q", tt.in, err, tt.wantErr)
			}
		})
	}
}

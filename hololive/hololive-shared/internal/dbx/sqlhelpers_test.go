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

import "testing"

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresPlaceholders(tt.in); got != tt.want {
				t.Errorf("PostgresPlaceholders(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
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

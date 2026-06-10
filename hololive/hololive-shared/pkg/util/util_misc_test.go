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

package util

import (
	"errors"
	"fmt"
	"testing"

	"github.com/valkey-io/valkey-go"
)

func TestMax(t *testing.T) {
	t.Parallel()

	if got := Max(10, 3); got != 10 {
		t.Fatalf("Max(10, 3) = %d, want 10", got)
	}
	if got := Max(-1, 5); got != 5 {
		t.Fatalf("Max(-1, 5) = %d, want 5", got)
	}
}

func TestMin(t *testing.T) {
	t.Parallel()

	if got := Min(10, 3); got != 3 {
		t.Fatalf("Min(10, 3) = %d, want 3", got)
	}
	if got := Min(-1, 5); got != -1 {
		t.Fatalf("Min(-1, 5) = %d, want -1", got)
	}
}

func TestUnique(t *testing.T) {
	t.Parallel()

	got := Unique([]int{3, 1, 3, 2, 1, 4, 2})
	want := []int{3, 1, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("len(Unique()) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Unique()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestFormatKoreanNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   int64
		want string
	}{
		{name: "less than ten-thousand", in: 500, want: "500"},
		{name: "exact ten-thousand", in: 10000, want: "1만"},
		{name: "with remainder", in: 12345, want: "1만 2345"},
		{name: "large exact", in: 250000, want: "25만"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatKoreanNumber(tc.in); got != tc.want {
				t.Fatalf("FormatKoreanNumber(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "trim and remove 짱", in: "  후부짱 ", want: "후부"},
		{name: "remove 쨩", in: "미코쨩", want: "미코"},
		{name: "no suffix", in: "스이세이", want: "스이세이"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeSuffix(tc.in); got != tc.want {
				t.Fatalf("NormalizeSuffix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsValkeyNil(t *testing.T) {
	t.Parallel()

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		if IsValkeyNil(nil) {
			t.Fatal("IsValkeyNil(nil) = true, want false")
		}
	})

	t.Run("direct valkey nil", func(t *testing.T) {
		t.Parallel()
		if !IsValkeyNil(valkey.Nil) {
			t.Fatal("IsValkeyNil(valkey.Nil) = false, want true")
		}
	})

	t.Run("wrapped valkey nil", func(t *testing.T) {
		t.Parallel()
		wrapped := fmt.Errorf("outer: %w", valkey.Nil)
		doubleWrapped := fmt.Errorf("outer2: %w", wrapped)
		if !IsValkeyNil(doubleWrapped) {
			t.Fatal("IsValkeyNil(doubleWrapped valkey.Nil) = false, want true")
		}
	})

	t.Run("non-valkey error", func(t *testing.T) {
		t.Parallel()
		if IsValkeyNil(errors.New("other")) {
			t.Fatal("IsValkeyNil(non-valkey) = true, want false")
		}
	})
}

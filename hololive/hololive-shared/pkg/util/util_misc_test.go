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

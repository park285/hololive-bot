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

package youtubedispatch

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
)

func TestTruncateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{name: "short text unchanged", in: "abc", maxLen: 5, want: "abc"},
		{name: "exact length unchanged", in: testMessageHello, maxLen: 5, want: testMessageHello},
		{name: "ascii truncated", in: "hello world", maxLen: 8, want: "hello..."},
		{name: "unicode truncated", in: "안녕하세요세계", maxLen: 6, want: "안녕하..."},
		{name: "minimum ellipsis", in: "abcdef", maxLen: 3, want: "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := deliverysql.TruncateString(tt.in, tt.maxLen); got != tt.want {
				t.Fatalf("deliverysql.TruncateString(%q, %d) = %q, want %q", tt.in, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestUniqueInt64s(t *testing.T) {
	t.Parallel()

	in := []int64{1, 2, 1, 3, 2, 4, 4}
	got := deliverysql.UniqueInt64s(in)
	want := []int64{1, 2, 3, 4}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deliverysql.UniqueInt64s() = %#v, want %#v", got, want)
	}

	if out := deliverysql.UniqueInt64s(nil); out != nil {
		t.Fatalf("deliverysql.UniqueInt64s(nil) = %#v, want nil", out)
	}
}

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

package iris

import "testing"

func TestDedupKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "normal", in: "abc-123", want: "iris:msg:{abc-123}"},
		{name: "trim spaces", in: "  id-1  ", want: "iris:msg:{id-1}"},
		{name: "empty", in: "", want: ""},
		{name: "spaces only", in: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DedupKey(tt.in)
			if got != tt.want {
				t.Fatalf("DedupKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveTokens(t *testing.T) {
	t.Parallel()

	webhook, bot := ResolveTokens("", "bot-token", "shared-token")
	if webhook != "shared-token" {
		t.Fatalf("webhook token = %q, want %q", webhook, "shared-token")
	}
	if bot != "bot-token" {
		t.Fatalf("bot token = %q, want %q", bot, "bot-token")
	}
}

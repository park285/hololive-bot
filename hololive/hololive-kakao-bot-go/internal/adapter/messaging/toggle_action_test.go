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

package messaging

import "testing"

func TestParseToggleAction(t *testing.T) {
	t.Parallel()

	aliases := map[string]string{
		"켜기":  "on",
		"on":  "on",
		"끄기":  "off",
		"off": "off",
		"상태":  "status",
	}

	tests := []struct {
		name     string
		args     []string
		fallback string
		want     string
	}{
		{name: "empty args returns fallback", args: nil, fallback: "status", want: "status"},
		{name: "empty slice returns fallback", args: []string{}, fallback: "status", want: "status"},
		{name: "mapped on", args: []string{"켜기"}, fallback: "status", want: "on"},
		{name: "mapped off ascii", args: []string{"off"}, fallback: "status", want: "off"},
		{name: "mapped status", args: []string{"상태"}, fallback: "status", want: "status"},
		{name: "unmapped first arg returns fallback", args: []string{"몰라"}, fallback: "status", want: "status"},
		{name: "uses only first arg", args: []string{"on", "끄기"}, fallback: "status", want: "on"},
		{name: "normalizes case", args: []string{"ON"}, fallback: "status", want: "on"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseToggleAction(tt.args, aliases, tt.fallback)
			if got != tt.want {
				t.Fatalf("parseToggleAction(%#v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

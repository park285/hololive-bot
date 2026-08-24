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
		"켜기":  testActionOn,
		"on":  testActionOn,
		"끄기":  testActionOff,
		"off": testActionOff,
		"상태":  testActionStatus,
	}

	tests := []struct {
		name     string
		args     []string
		fallback string
		want     string
	}{
		{name: "empty args returns fallback", args: nil, fallback: testActionStatus, want: testActionStatus},
		{name: "empty slice returns fallback", args: []string{}, fallback: testActionStatus, want: testActionStatus},
		{name: "mapped on", args: []string{"켜기"}, fallback: testActionStatus, want: testActionOn},
		{name: "mapped off ascii", args: []string{"off"}, fallback: testActionStatus, want: testActionOff},
		{name: "mapped status", args: []string{"상태"}, fallback: testActionStatus, want: testActionStatus},
		{name: "unmapped first arg returns fallback", args: []string{"몰라"}, fallback: testActionStatus, want: testActionStatus},
		{name: "uses only first arg", args: []string{"on", "끄기"}, fallback: testActionStatus, want: testActionOn},
		{name: "normalizes case", args: []string{"ON"}, fallback: testActionStatus, want: testActionOn},
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

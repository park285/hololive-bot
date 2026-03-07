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

package server

import "testing"

func TestSplitChannelIDs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single",
			raw:  "UC123",
			want: []string{"UC123"},
		},
		{
			name: "multiple",
			raw:  "UC123,UC456,UC789",
			want: []string{"UC123", "UC456", "UC789"},
		},
		{
			name: "spaces and tabs",
			raw:  " UC123,\tUC456 , UC789 ",
			want: []string{"UC123", "UC456", "UC789"},
		},
		{
			name: "empty and commas",
			raw:  ",,UC123,,",
			want: []string{"UC123"},
		},
		{
			name: "empty input",
			raw:  "",
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitChannelIDs(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("SplitChannelIDs(%q) len=%d want=%d got=%v", tt.raw, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("SplitChannelIDs(%q)[%d]=%q want=%q", tt.raw, i, got[i], tt.want[i])
				}
			}
		})
	}
}

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

package matcher

import (
	"testing"
)

func TestParseNameWithOrg(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedName string
		expectedOrg  string
	}{
		{
			name:         "without org",
			input:        "미코",
			expectedName: "미코",
			expectedOrg:  "",
		},
		{
			name:         "with org Hololive",
			input:        "미코 (Hololive)",
			expectedName: "미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "with org Nijisanji",
			input:        "미코 (Nijisanji)",
			expectedName: "미코",
			expectedOrg:  "Nijisanji",
		},
		{
			name:         "with org VSPO",
			input:        "치호 (VSPO)",
			expectedName: "치호",
			expectedOrg:  "VSPO",
		},
		{
			name:         "with org Indie",
			input:        "사쿠나 (Indie)",
			expectedName: "사쿠나",
			expectedOrg:  "Indie",
		},
		{
			name:         "with extra spaces",
			input:        "미코  (Hololive) ",
			expectedName: "미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "name with spaces",
			input:        "사쿠라 미코 (Hololive)",
			expectedName: "사쿠라 미코",
			expectedOrg:  "Hololive",
		},
		{
			name:         "empty input",
			input:        "",
			expectedName: "",
			expectedOrg:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotOrg := ParseNameWithOrg(tt.input)
			if gotName != tt.expectedName {
				t.Errorf("ParseNameWithOrg(%q) name = %q, want %q", tt.input, gotName, tt.expectedName)
			}

			if gotOrg != tt.expectedOrg {
				t.Errorf("ParseNameWithOrg(%q) org = %q, want %q", tt.input, gotOrg, tt.expectedOrg)
			}
		})
	}
}

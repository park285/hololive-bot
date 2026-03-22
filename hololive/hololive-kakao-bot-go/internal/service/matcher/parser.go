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
	"regexp"
	"strings"
)

// ParseNameWithOrg: "이름 (그룹)" 형식의 입력을 파싱합니다.
// Input: "미코 (Nijisanji)" → Output: name="미코", org="Nijisanji"
// Input: "미코" → Output: name="미코", org="".
func ParseNameWithOrg(input string) (name, org string) {
	re := regexp.MustCompile(`^(.+?)\s*\(([^)]+)\)\s*$`)

	matches := re.FindStringSubmatch(input)
	if len(matches) == 3 {
		name = strings.TrimSpace(matches[1])
		org = strings.TrimSpace(matches[2])

		if org == "Indie" {
			org = "Independents"
		}

		return name, org
	}

	return input, ""
}

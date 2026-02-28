package matcher

import (
	"regexp"
	"strings"
)

// ParseNameWithOrg: "이름 (그룹)" 형식의 입력을 파싱합니다.
// Input: "미코 (Nijisanji)" → Output: name="미코", org="Nijisanji"
// Input: "미코" → Output: name="미코", org=""
func ParseNameWithOrg(input string) (name, org string) {
	re := regexp.MustCompile(`^(.+?)\s*\(([^)]+)\)\s*$`)
	matches := re.FindStringSubmatch(input)
	if len(matches) == 3 {
		return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2])
	}
	return input, ""
}

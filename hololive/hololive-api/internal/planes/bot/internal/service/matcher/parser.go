package matcher

import (
	"regexp"
	"strings"
)

var nameWithOrgRe = regexp.MustCompile(`^(.+?)\s*\(([^)]+)\)\s*$`)

func ParseNameWithOrg(input string) (name, org string) {
	matches := nameWithOrgRe.FindStringSubmatch(input)
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

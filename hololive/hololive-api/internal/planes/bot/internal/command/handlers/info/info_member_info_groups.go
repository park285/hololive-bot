package info

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *MemberInfoCommand) memberGroups(member *domain.Member) []string {
	if member == nil {
		return nil
	}

	if len(member.Units) != 0 {
		return member.Units
	}

	if group, ok := orgDirectoryGroups[member.Org]; ok {
		return []string{group}
	}

	if member.Org != "" {
		return []string{member.Org + " (기수 미등록)"}
	}

	return nil
}

func PrimaryMemberName(member *domain.Member) string {
	if member == nil {
		return ""
	}

	if name := strings.TrimSpace(member.NameKo); name != "" {
		return name
	}

	return member.Name
}

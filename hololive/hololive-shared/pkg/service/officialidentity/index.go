package officialidentity

import (
	"slices"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Index map[string][]string

func Build(membersData domain.MemberDataProvider) Index {
	candidates := make(map[string]map[string]struct{})

	if membersData == nil {
		return Index{}
	}

	for _, member := range membersData.GetAllMembers() {
		addMemberCandidates(candidates, member)
	}

	index := make(Index, len(candidates))
	for name, channelIDs := range candidates {
		resolved := make([]string, 0, len(channelIDs))
		for channelID := range channelIDs {
			resolved = append(resolved, channelID)
		}

		slices.Sort(resolved)

		index[name] = resolved
	}

	return index
}

func (index Index) Resolve(name string) string {
	channelIDs := index[stringutil.Normalize(name)]
	if len(channelIDs) != 1 {
		return ""
	}

	return channelIDs[0]
}

func DisplayNames(membersData domain.MemberDataProvider, officialNames []string, hostChannelID string) []string {
	index := Build(membersData)

	hostChannelID = strings.TrimSpace(hostChannelID)

	out := make([]string, 0, len(officialNames))
	seen := make(map[string]struct{}, len(officialNames))

	for _, name := range officialNames {
		label := displayName(membersData, index, name, hostChannelID)
		if label == "" {
			continue
		}

		if _, exists := seen[label]; exists {
			continue
		}

		seen[label] = struct{}{}
		out = append(out, label)
	}

	return out
}

func Format(names []string) string {
	return strings.Join(names, ", ")
}

func displayName(membersData domain.MemberDataProvider, index Index, officialName, hostChannelID string) string {
	name := strings.TrimSpace(officialName)
	if name == "" {
		return ""
	}

	channelID := index.Resolve(name)
	if isHostCollaboChannel(channelID, hostChannelID) {
		return ""
	}

	return mappedCollaboDisplayName(membersData, channelID, name)
}

func isHostCollaboChannel(channelID, hostChannelID string) bool {
	return channelID != "" && channelID == hostChannelID
}

func mappedCollaboDisplayName(membersData domain.MemberDataProvider, channelID, officialName string) string {
	if channelID == "" || membersData == nil {
		return officialName
	}

	member := membersData.FindMemberByChannelID(channelID)
	if member == nil {
		return officialName
	}

	return firstNonEmptyName(member.ShortKoreanName, member.NameKo, member.Name, officialName)
}

func firstNonEmptyName(values ...string) string {
	for _, value := range values {
		if label := strings.TrimSpace(value); label != "" {
			return label
		}
	}

	return ""
}

func addMemberCandidates(candidates map[string]map[string]struct{}, member *domain.Member) {
	if member == nil || member.ChannelID == "" {
		return
	}

	addIdentity(candidates, member.Name, member.ChannelID)
	addIdentity(candidates, member.NameJa, member.ChannelID)
	addIdentity(candidates, member.NameKo, member.ChannelID)
	addIdentity(candidates, member.ShortKoreanName, member.ChannelID)

	if member.Aliases == nil {
		return
	}

	for _, alias := range member.Aliases.Ko {
		addIdentity(candidates, alias, member.ChannelID)
	}

	for _, alias := range member.Aliases.Ja {
		addIdentity(candidates, alias, member.ChannelID)
	}
}

func addIdentity(candidates map[string]map[string]struct{}, name, channelID string) {
	normalized := stringutil.Normalize(name)
	if normalized == "" {
		return
	}

	channelIDs := candidates[normalized]
	if channelIDs == nil {
		channelIDs = make(map[string]struct{})
		candidates[normalized] = channelIDs
	}

	channelIDs[channelID] = struct{}{}
}

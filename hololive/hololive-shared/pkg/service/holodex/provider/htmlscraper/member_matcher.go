package htmlscraper

import (
	"sort"

	"github.com/park285/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type officialScheduleIdentityIndex map[string][]string

func buildOfficialScheduleIdentityIndex(membersData domain.MemberDataProvider) officialScheduleIdentityIndex {
	candidates := make(map[string]map[string]struct{})
	if membersData == nil {
		return officialScheduleIdentityIndex{}
	}

	for _, member := range membersData.GetAllMembers() {
		addOfficialScheduleMemberCandidates(candidates, member)
	}

	index := make(officialScheduleIdentityIndex, len(candidates))
	for name, channelIDs := range candidates {
		resolved := make([]string, 0, len(channelIDs))
		for channelID := range channelIDs {
			resolved = append(resolved, channelID)
		}
		sort.Strings(resolved)
		index[name] = resolved
	}
	return index
}

func addOfficialScheduleMemberCandidates(candidates map[string]map[string]struct{}, member *domain.Member) {
	if member == nil || member.ChannelID == "" {
		return
	}

	addOfficialScheduleIdentity(candidates, member.Name, member.ChannelID)
	addOfficialScheduleIdentity(candidates, member.NameJa, member.ChannelID)
	addOfficialScheduleIdentity(candidates, member.NameKo, member.ChannelID)
	addOfficialScheduleIdentity(candidates, member.ShortKoreanName, member.ChannelID)
	if member.Aliases == nil {
		return
	}
	for _, alias := range member.Aliases.Ko {
		addOfficialScheduleIdentity(candidates, alias, member.ChannelID)
	}
	for _, alias := range member.Aliases.Ja {
		addOfficialScheduleIdentity(candidates, alias, member.ChannelID)
	}
}

func addOfficialScheduleIdentity(candidates map[string]map[string]struct{}, name, channelID string) {
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

func (index officialScheduleIdentityIndex) resolve(name string) string {
	channelIDs := index[stringutil.Normalize(name)]
	if len(channelIDs) != 1 {
		return ""
	}
	return channelIDs[0]
}

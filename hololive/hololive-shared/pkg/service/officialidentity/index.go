package officialidentity

import (
	"fmt"
	"slices"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Index map[string][]string

// Build는 멤버 데이터에서 공식 표기명→채널 색인을 만든다. 멤버 적재 실패는 빈 색인으로 바꾸지 않고 오류로 돌려준다
// (DEC-20260926-hololive-source-fallbacks-retirement). 멤버 데이터 없이 구성한 호출자(membersData nil)는 빈 색인이다.
func Build(membersData domain.MemberDataProvider) (Index, error) {
	candidates := make(map[string]map[string]struct{})

	if membersData == nil {
		return Index{}, nil
	}

	members, err := membersData.LoadAllMembers()
	if err != nil {
		return nil, fmt.Errorf("build official identity index: %w", err)
	}

	for _, member := range members {
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

	return index, nil
}

func (index Index) Resolve(name string) string {
	channelIDs := index[stringutil.Normalize(name)]
	if len(channelIDs) != 1 {
		return ""
	}

	return channelIDs[0]
}

// DisplayNames는 공식 표기명 목록을 표시명으로 바꾼다. 이름이 없으면 멤버를 적재하지 않는다.
func DisplayNames(membersData domain.MemberDataProvider, officialNames []string, hostChannelID string) ([]string, error) {
	if len(officialNames) == 0 {
		return nil, nil
	}

	index, err := Build(membersData)
	if err != nil {
		return nil, fmt.Errorf("display names: %w", err)
	}

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

	return out, nil
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

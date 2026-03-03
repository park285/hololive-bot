package command

import (
	"context"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

func (c *MemberInfoCommand) renderMemberDirectory(ctx context.Context, cmdCtx *domain.CommandContext) error {
	provider := c.Deps().MembersData.WithContext(ctx)
	activeMembers := c.filterActiveMembers(provider.GetAllMembers())
	if len(activeMembers) == 0 {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrNoMemberInfoFound)
	}

	groupEntries := c.buildGroupEntries(ctx, activeMembers)
	if len(groupEntries) == 0 {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrNoMemberInfoFound)
	}

	ordered := c.sortGroupsByPreference(groupEntries)
	message := c.Deps().Formatter.MemberDirectory(ctx, ordered, len(activeMembers))
	if stringutil.TrimSpace(message) == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrCannotDisplayMemberInfo)
	}

	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *MemberInfoCommand) filterActiveMembers(members []*domain.Member) []*domain.Member {
	activeMembers := make([]*domain.Member, 0, len(members))
	for _, member := range members {
		if member != nil && !member.IsGraduated {
			activeMembers = append(activeMembers, member)
		}
	}
	return activeMembers
}

func (c *MemberInfoCommand) buildGroupEntries(ctx context.Context, members []*domain.Member) map[string]map[string]adapter.MemberDirectoryEntry {
	groupEntries := make(map[string]map[string]adapter.MemberDirectoryEntry)

	for _, member := range members {
		if member == nil {
			continue
		}

		groups := c.memberGroups(ctx, member)
		if len(groups) == 0 {
			groups = []string{defaultMemberDirectoryGroup}
		}

		entry := adapter.MemberDirectoryEntry{
			PrimaryName:   primaryMemberName(member),
			SecondaryName: member.Name,
		}

		for _, group := range groups {
			if groupEntries[group] == nil {
				groupEntries[group] = make(map[string]adapter.MemberDirectoryEntry)
			}
			groupEntries[group][member.Name] = entry
		}
	}

	return groupEntries
}

func (c *MemberInfoCommand) sortGroupsByPreference(groupEntries map[string]map[string]adapter.MemberDirectoryEntry) []adapter.MemberDirectoryGroup {
	ordered := make([]adapter.MemberDirectoryGroup, 0, len(groupEntries))
	used := make(map[string]bool)

	for _, groupName := range memberDirectoryPreferredOrder {
		if bucket, ok := groupEntries[groupName]; ok {
			ordered = append(ordered, buildMemberDirectoryGroup(groupName, bucket))
			used[groupName] = true
		}
	}

	remaining := make([]string, 0, len(groupEntries))
	for name := range groupEntries {
		if !used[name] {
			remaining = append(remaining, name)
		}
	}
	slices.Sort(remaining)

	for _, name := range remaining {
		ordered = append(ordered, buildMemberDirectoryGroup(name, groupEntries[name]))
	}

	return ordered
}

func buildMemberDirectoryGroup(groupName string, entries map[string]adapter.MemberDirectoryEntry) adapter.MemberDirectoryGroup {
	list := make([]adapter.MemberDirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry)
	}
	slices.SortStableFunc(list, func(a, b adapter.MemberDirectoryEntry) int {
		if a.PrimaryName < b.PrimaryName {
			return -1
		}
		if a.PrimaryName > b.PrimaryName {
			return 1
		}
		return 0
	})
	return adapter.MemberDirectoryGroup{
		GroupName: groupName,
		Members:   list,
	}
}

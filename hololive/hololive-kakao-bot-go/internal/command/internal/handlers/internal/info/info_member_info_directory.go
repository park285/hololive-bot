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

package info

import (
	"context"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"

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

		entry := adapter.MemberDirectoryEntry{
			PrimaryName:   PrimaryMemberName(member),
			SecondaryName: member.Name,
		}
		addMemberDirectoryEntry(groupEntries, member.Name, entry, c.directoryGroupsForMember(ctx, member))
	}

	return groupEntries
}

func (c *MemberInfoCommand) directoryGroupsForMember(ctx context.Context, member *domain.Member) []string {
	groups := c.memberGroups(ctx, member)
	if len(groups) == 0 {
		return []string{DefaultMemberDirectoryGroup}
	}

	return groups
}

func addMemberDirectoryEntry(
	groupEntries map[string]map[string]adapter.MemberDirectoryEntry,
	memberName string,
	entry adapter.MemberDirectoryEntry,
	groups []string,
) {
	for _, group := range groups {
		if groupEntries[group] == nil {
			groupEntries[group] = make(map[string]adapter.MemberDirectoryEntry)
		}

		groupEntries[group][memberName] = entry
	}
}

func (c *MemberInfoCommand) sortGroupsByPreference(groupEntries map[string]map[string]adapter.MemberDirectoryEntry) []adapter.MemberDirectoryGroup {
	ordered := make([]adapter.MemberDirectoryGroup, 0, len(groupEntries))
	used := make(map[string]bool)

	for _, groupName := range memberDirectoryPreferredOrder {
		if bucket, ok := groupEntries[groupName]; ok {
			ordered = append(ordered, BuildMemberDirectoryGroup(groupName, bucket))
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
		ordered = append(ordered, BuildMemberDirectoryGroup(name, groupEntries[name]))
	}

	return ordered
}

func BuildMemberDirectoryGroup(groupName string, entries map[string]adapter.MemberDirectoryEntry) adapter.MemberDirectoryGroup {
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

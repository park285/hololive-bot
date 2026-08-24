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

package member

import (
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Cache) snapshotOwnedChannelMemberLocked(
	channelID string,
	cached *domain.Member,
	generation uint64,
) *domain.Member {
	if cached.ChannelID != channelID {
		return nil
	}

	return c.snapshotOwnedPointMemberLocked(cached, generation, func(current *domain.Member) bool {
		return current.ChannelID == channelID
	})
}

func (c *Cache) snapshotOwnedNameMemberLocked(
	name string,
	cached *domain.Member,
	generation uint64,
) *domain.Member {
	if cached.Name != name {
		return nil
	}

	return c.snapshotOwnedPointMemberLocked(cached, generation, func(current *domain.Member) bool {
		return current.Name == name
	})
}

func (c *Cache) snapshotOwnedAliasMemberLocked(
	alias string,
	cached *domain.Member,
	generation uint64,
) *domain.Member {
	if !memberMatchesPointAlias(cached, alias) {
		return nil
	}

	return c.snapshotOwnedPointMemberLocked(cached, generation, func(current *domain.Member) bool {
		return memberMatchesPointAlias(current, alias)
	})
}

func (c *Cache) snapshotOwnedPointMemberLocked(
	cached *domain.Member,
	generation uint64,
	matches func(*domain.Member) bool,
) *domain.Member {
	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) {
		return pointMemberWithoutSnapshot(cached, generation)
	}

	if snap.generation != generation {
		return nil
	}

	for _, current := range snap.members {
		if current != nil && matches(current) && samePointMemberIdentity(current, cached) {
			return current
		}
	}

	return nil
}

func pointMemberWithoutSnapshot(cached *domain.Member, generation uint64) *domain.Member {
	if generation != 0 {
		return nil
	}

	return cached
}

func samePointMemberIdentity(current, cached *domain.Member) bool {
	if current.ID != 0 || cached.ID != 0 {
		return current.ID != 0 && current.ID == cached.ID
	}

	if current.ChannelID != "" || cached.ChannelID != "" {
		return current.ChannelID != "" && current.ChannelID == cached.ChannelID
	}

	return current.Name == cached.Name
}

func memberMatchesPointAlias(member *domain.Member, alias string) bool {
	if member == nil {
		return false
	}

	if strings.EqualFold(member.Name, alias) ||
		strings.EqualFold(member.NameJa, alias) ||
		strings.EqualFold(member.NameKo, alias) {
		return true
	}

	return slices.Contains(member.GetAllAliases(), alias)
}

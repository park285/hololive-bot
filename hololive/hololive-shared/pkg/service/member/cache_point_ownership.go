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

	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) || snap.generation != generation {
		return nil
	}

	representative := snap.pointLookup().representatives[channelID]
	if representative == nil || !samePointMemberIdentity(representative, cached) {
		return nil
	}

	return representative
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

	for _, current := range snap.pointLookup().byIdentity[pointKey(cached)] {
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

	return member.Aliases != nil &&
		(slices.Contains(member.Aliases.Ko, alias) || slices.Contains(member.Aliases.Ja, alias))
}

// ID가 있으면 ID만 식별에 사용한다. ID가 없는 호환 데이터는 채널, 이름 순이다.
// 각 버킷의 순서를 보존하여 중복 ID를 가진 테스트/호환 데이터도 기존 탐색과 일치한다.
type pointMemberKey struct {
	id        int
	channelID string
	name      string
}

type memberPointIndex struct {
	byIdentity      map[pointMemberKey][]*domain.Member
	representatives map[string]*domain.Member
}

func pointKey(member *domain.Member) pointMemberKey {
	if member.ID != 0 {
		return pointMemberKey{id: member.ID}
	}
	if member.ChannelID != "" {
		return pointMemberKey{channelID: member.ChannelID}
	}
	return pointMemberKey{name: member.Name}
}

func buildMemberPointIndex(members []*domain.Member) *memberPointIndex {
	index := &memberPointIndex{
		byIdentity:      make(map[pointMemberKey][]*domain.Member, len(members)),
		representatives: channelRepresentatives(members),
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		key := pointKey(member)
		index.byIdentity[key] = append(index.byIdentity[key], member)
	}
	return index
}

func (s *allMembersState) pointLookup() *memberPointIndex {
	// 런타임은 게시 전에 준비한다. 직접 생성한 스냅샷도 동시 읽기에서 한 번만 구성한다.
	s.pointIndexOnce.Do(func() {
		if s.pointIndex == nil {
			s.pointIndex = buildMemberPointIndex(s.members)
		}
	})
	return s.pointIndex
}

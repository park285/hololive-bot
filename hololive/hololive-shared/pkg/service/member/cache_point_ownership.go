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

	for _, current := range snap.pointLookup().byID[cached.ID] {
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

// samePointMemberIdentity는 영속 ID 하나로만 같은 멤버인지 본다. 스냅샷은 repository가 members.id와 함께 적재하므로
// ID 없는 멤버가 나오지 않는다. ID가 없으면 채널, 이름 순으로 대신 식별하던 호환 체인은 지웠다(stack-audit 2026-09-26
// T11 holo-member-point-identity-fallback-chain). ID가 없는 cached 값은 소유를 인정받지 못해 캐시에 다시 쓰이지 않는다.
func samePointMemberIdentity(current, cached *domain.Member) bool {
	return current.ID > 0 && current.ID == cached.ID
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

// memberPointIndex는 영속 ID를 키로 쓴다. 각 버킷은 snapshot 순서를 보존하고, ID가 없는 멤버는 인덱스에 넣지 않는다.
type memberPointIndex struct {
	byID            map[int][]*domain.Member
	representatives map[string]*domain.Member
}

func buildMemberPointIndex(members []*domain.Member) *memberPointIndex {
	index := &memberPointIndex{
		byID:            make(map[int][]*domain.Member, len(members)),
		representatives: ChannelRepresentatives(members),
	}
	for _, member := range members {
		if member == nil || member.ID <= 0 {
			continue
		}

		index.byID[member.ID] = append(index.byID[member.ID], member)
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

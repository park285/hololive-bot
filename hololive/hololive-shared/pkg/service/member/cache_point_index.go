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

// aliasOwner는 repository FindByAlias(repository_query_0064_03.sql)와 같은 기준으로 snapshot에서 별칭 주인을 고른다.
// 공식 이름은 대소문자를 무시하고, 명시 별칭은 정확히 일치해야 하며, 여러 명이 맞으면 SQL의 ORDER BY id처럼 가장 작은
// 영속 ID가 이긴다.
func (s *allMembersState) aliasOwner(alias string) *domain.Member {
	var owner *domain.Member

	for _, member := range s.members {
		if memberMatchesPointAlias(member, alias) && (owner == nil || member.ID < owner.ID) {
			owner = member
		}
	}

	return owner
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

// memberPointIndex는 snapshot의 채널 대표(가장 작은 영속 ID)를 담는다. Snapshot과 함께 불변으로 게시된다.
type memberPointIndex struct {
	representatives map[string]*domain.Member
}

func buildMemberPointIndex(members []*domain.Member) *memberPointIndex {
	return &memberPointIndex{representatives: ChannelRepresentatives(members)}
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

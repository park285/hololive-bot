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
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 게시 뒤에는 읽기만 하므로 잠금 없이 공유한다.
type memberSnapshotIndex struct {
	// channelRepresentatives는 채널별 가장 작은 영속 ID 멤버다(find_by_channel_id.sql의 ORDER BY id).
	channelRepresentatives map[string]*domain.Member
	// byName은 english_name 정확 일치다. 같은 이름이 여럿이면 find_by_name.sql의 ORDER BY id처럼 가장 작은
	// 영속 ID가 이긴다.
	byName map[string]*domain.Member
	// pointAliasByName과 pointAliasByExact는 find_by_alias.sql의 단건 별칭 규칙이다. 공식 이름(영·일·한)은
	// 대소문자 무관(EqualFold) 비교, 명시 별칭은 정확 일치이며, 키마다 가장 작은 영속 ID를 담는다.
	pointAliasByName  map[string]*domain.Member
	pointAliasByExact map[string]*domain.Member
	channelIDs        []string
}

func newMemberSnapshotIndex(members []*domain.Member) ([]*domain.Member, *memberSnapshotIndex) {
	snapshot := make([]*domain.Member, 0, len(members))
	index := &memberSnapshotIndex{
		byName:            make(map[string]*domain.Member, len(members)),
		pointAliasByName:  make(map[string]*domain.Member, len(members)*3),
		pointAliasByExact: make(map[string]*domain.Member, len(members)*2),
		channelIDs:        make([]string, 0, len(members)),
	}

	for _, member := range members {
		if member == nil {
			continue
		}

		snapshot = append(snapshot, member)

		if member.ChannelID != "" {
			index.channelIDs = append(index.channelIDs, member.ChannelID)
		}

		keepSmallestID(index.byName, member.Name, member)
		index.addPointAliases(member)
	}

	index.channelRepresentatives = ChannelRepresentatives(snapshot)

	return snapshot, index
}

// addPointAliases는 SQL lower(name) = lower($1)·aliases ? $1 비교와 같게, 빈 값도 키로 둔다.
func (index *memberSnapshotIndex) addPointAliases(member *domain.Member) {
	for _, name := range [...]string{member.Name, member.NameJa, member.NameKo} {
		keepSmallestID(index.pointAliasByName, foldKey(name), member)
	}

	if member.Aliases == nil {
		return
	}

	for _, alias := range member.Aliases.Ko {
		keepSmallestID(index.pointAliasByExact, alias, member)
	}

	for _, alias := range member.Aliases.Ja {
		keepSmallestID(index.pointAliasByExact, alias, member)
	}
}

func keepSmallestID(index map[string]*domain.Member, key string, member *domain.Member) {
	if current := index[key]; current == nil || member.ID < current.ID {
		index[key] = member
	}
}

// aliasOwner는 repository FindByAlias(find_by_alias.sql)와 같은 기준으로 snapshot에서 별칭 주인을 고른다.
// 공식 이름은 대소문자를 무시하고, 명시 별칭은 정확히 일치해야 하며, 여러 명이 맞으면 SQL의 ORDER BY id처럼 가장 작은
// 영속 ID가 이긴다.
func (index *memberSnapshotIndex) aliasOwner(alias string) *domain.Member {
	byName := index.pointAliasByName[foldKey(alias)]
	byAlias := index.pointAliasByExact[alias]

	if byName == nil || (byAlias != nil && byAlias.ID < byName.ID) {
		return byAlias
	}

	return byName
}

// foldKey는 strings.EqualFold의 동치류 대표형이다. EqualFold는 rune마다 unicode.SimpleFold 궤도가 같은지 보므로, 각 rune을
// 궤도의 가장 작은 rune으로 바꾸면 EqualFold(a, b)와 foldKey(a) == foldKey(b)가 같다. Strings.ToLower는 Kelvin 기호(K)나
// long s(ſ) 같은 궤도를 다르게 접으므로 쓰지 않는다. 잘못된 UTF-8 바이트는 EqualFold처럼 utf8.RuneError로 읽는다.
func foldKey(value string) string {
	if foldKeyStable(value) {
		return value
	}

	var builder strings.Builder

	builder.Grow(len(value))

	for _, r := range value {
		builder.WriteRune(foldRune(r))
	}

	return builder.String()
}

func foldKeyStable(value string) bool {
	for index, r := range value {
		if r == utf8.RuneError {
			if _, size := utf8.DecodeRuneInString(value[index:]); size == 1 {
				return false
			}
		}

		if foldRune(r) != r {
			return false
		}
	}

	return true
}

func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'a' <= r && r <= 'z' {
			return r - ('a' - 'A')
		}

		return r
	}

	smallest := r
	for folded := unicode.SimpleFold(r); folded != r; folded = unicode.SimpleFold(folded) {
		smallest = min(smallest, folded)
	}

	return smallest
}

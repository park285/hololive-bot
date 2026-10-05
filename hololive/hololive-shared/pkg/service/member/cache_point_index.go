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

// memberSnapshotIndex는 snapshot 멤버 목록에서 한 번 만들어 snapshot과 함께 불변으로 게시하는 조회 색인이다. 게시 뒤에는
// 읽기만 하므로 잠금 없이 공유한다. 각 색인은 예전 전체 순회 규칙과 같은 결과를 내도록 만든다.
type memberSnapshotIndex struct {
	// channelRepresentatives는 채널별 가장 작은 영속 ID 멤버다(repository_query_0032_01.sql의 ORDER BY id).
	channelRepresentatives map[string]*domain.Member
	// byName은 english_name 정확 일치다. 같은 이름이 여럿이면 repository_query_0048_02.sql의 ORDER BY id처럼 가장 작은
	// 영속 ID가 이긴다.
	byName map[string]*domain.Member
	// pointAliasByName과 pointAliasByExact는 repository_query_0064_03.sql의 단건 별칭 규칙이다. 공식 이름(영·일·한)은
	// 대소문자 무관(EqualFold) 비교, 명시 별칭은 정확 일치이며, 키마다 가장 작은 영속 ID를 담는다.
	pointAliasByName  map[string]*domain.Member
	pointAliasByExact map[string]*domain.Member
	// namesByKey와 aliasesByKey는 다건 조회 색인이다. 키는 앞뒤 공백을 지운 값의 EqualFold 대표형이고, 값은 snapshot
	// 순서를 지키며 멤버마다 한 번만 담는다.
	namesByKey   map[string][]*domain.Member
	aliasesByKey map[string][]*domain.Member
	channelIDs   []string
}

// newMemberSnapshotIndex는 nil 멤버를 뺀 snapshot 목록과 그 색인을 만든다.
func newMemberSnapshotIndex(members []*domain.Member) ([]*domain.Member, *memberSnapshotIndex) {
	snapshot := make([]*domain.Member, 0, len(members))
	index := &memberSnapshotIndex{
		byName:            make(map[string]*domain.Member, len(members)),
		pointAliasByName:  make(map[string]*domain.Member, len(members)*3),
		pointAliasByExact: make(map[string]*domain.Member, len(members)*2),
		namesByKey:        make(map[string][]*domain.Member, len(members)*3),
		aliasesByKey:      make(map[string][]*domain.Member, len(members)*2),
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
		appendDistinct(index.namesByKey, member, member.Name, member.NameJa, member.NameKo)

		if member.Aliases != nil {
			appendDistinct(index.aliasesByKey, member, member.Aliases.Ko...)
			appendDistinct(index.aliasesByKey, member, member.Aliases.Ja...)
		}
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

// appendDistinct는 값들의 검색 키마다 멤버를 한 번만 붙인다. 공백뿐인 값은 공백 아닌 질의와 일치할 수 없으므로 뺀다.
func appendDistinct(index map[string][]*domain.Member, member *domain.Member, values ...string) {
	for _, value := range values {
		key, ok := searchKey(value)
		if !ok {
			continue
		}

		bucket := index[key]
		if len(bucket) > 0 && bucket[len(bucket)-1] == member {
			continue
		}

		index[key] = append(bucket, member)
	}
}

// aliasOwner는 repository FindByAlias(repository_query_0064_03.sql)와 같은 기준으로 snapshot에서 별칭 주인을 고른다.
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

// membersByName과 membersByAlias의 결과는 게시된 색인을 공유하므로 호출자에게 넘기기 전에 복사한다.
func (index *memberSnapshotIndex) membersByName(key string) []*domain.Member {
	return index.namesByKey[key]
}

func (index *memberSnapshotIndex) membersByAlias(key string) []*domain.Member {
	return index.aliasesByKey[key]
}

// searchKey는 다건 조회 질의와 색인 값을 같은 키로 바꾼다. 앞뒤 공백을 지운 값이 비면 false다.
func searchKey(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}

	return foldKey(trimmed), true
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

// foldKeyStable은 모든 rune이 이미 대표형인 유효 UTF-8 문자열인지 본다. 대표형이면 할당 없이 그대로 쓴다.
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

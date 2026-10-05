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

package domain

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

type Aliases struct {
	Ko []string `json:"ko"`
	Ja []string `json:"ja"`
}

// Member는 등록된 멤버의 식별자와 기본 정보를 제공한다. 미등록 선택 정보는 비워 둔다.
type Member struct {
	ID              int        `json:"id"`
	ChannelID       string     `json:"channelId"`
	Name            string     `json:"name"`
	Aliases         *Aliases   `json:"aliases,omitempty"`
	NameJa          string     `json:"nameJa,omitempty"`
	NameKo          string     `json:"nameKo,omitempty"`
	ShortKoreanName string     `json:"shortKoreanName,omitempty"`
	IsGraduated     bool       `json:"isGraduated"`
	Photo           string     `json:"photo,omitempty"`          // YouTube 프로필 이미지 URL (고화질)
	Units           []string   `json:"units,omitempty"`          // 기수·유닛 표시명. 복수 소속을 보존한다.
	OfficialURL     string     `json:"officialUrl,omitempty"`    // 확인된 공식 멤버 페이지.
	Org             string     `json:"org,omitempty"`            // 그룹명 (Hololive, Nijisanji, VSPO, Independents)
	Suborg          string     `json:"suborg,omitempty"`         // 서브그룹 (예: EN, JP, KR)
	SyncSource      string     `json:"sync_source,omitempty"`    // 동기화 소스 (holodex, manual)
	ChzzkChannelID  string     `json:"chzzkChannelId,omitempty"` // Chzzk 채널 ID
	TwitchUserID    string     `json:"twitchUserId,omitempty"`   // Twitch 사용자 ID (불변)
	Birthday        *time.Time `json:"birthday,omitempty"`
	DebutDate       *time.Time `json:"debutDate,omitempty"`
}

func (m *Member) GetAllAliases() []string {
	if m.Aliases == nil {
		return []string{}
	}

	all := make([]string, 0, len(m.Aliases.Ko)+len(m.Aliases.Ja))

	all = append(all, m.Aliases.Ko...)
	all = append(all, m.Aliases.Ja...)

	return all
}

func (m *Member) HasAlias(name string) bool {
	aliases := m.GetAllAliases()
	return slices.Contains(aliases, name)
}

func (m *Member) GetOrg() string {
	if m.Org == "" {
		return "Hololive"
	}

	return m.Org
}

// DisplayName은 명령 응답에 쓰는 멤버 표시명을 돌려준다. 순서는 members 정본의 short_korean_name→korean_name→english_name이며,
// 방송기록 SQL과 같다. 표시 전용이므로 식별·검색 키로 쓰지 않는다.
func (m *Member) DisplayName() string {
	return cmp.Or(m.ShortKoreanName, m.NameKo, m.Name)
}

// QualifiedName은 동명이인을 고르는 명령 인자 형식 "english_name (그룹)"을 돌려준다. 이 형식은 matcher가 검색 키로 해석한다.
func (m *Member) QualifiedName() string {
	return fmt.Sprintf("%s (%s)", m.Name, m.GetOrg())
}

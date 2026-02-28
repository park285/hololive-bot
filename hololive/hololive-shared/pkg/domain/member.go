package domain

import (
	"fmt"
	"slices"
)

// Aliases: 멤버의 국가별(한국어, 일본어) 별명 목록
type Aliases struct {
	Ko []string `json:"ko"`
	Ja []string `json:"ja"`
}

// Member: Hololive 멤버의 기본 정보(ID, 채널, 이름 등)를 담는 구조체
type Member struct {
	ID             int      `json:"id,omitempty"`
	ChannelID      string   `json:"channelId"`
	Name           string   `json:"name"`
	Aliases        *Aliases `json:"aliases,omitempty"`
	NameJa         string   `json:"nameJa,omitempty"`
	NameKo         string   `json:"nameKo,omitempty"`
	IsGraduated    bool     `json:"isGraduated,omitempty"`
	Photo          string   `json:"photo,omitempty"`          // YouTube 프로필 이미지 URL (고화질)
	Org            string   `json:"org,omitempty"`            // 그룹명 (Hololive, Nijisanji, VSPO, Indie)
	Suborg         string   `json:"suborg,omitempty"`         // 서브그룹 (예: EN, JP, KR)
	SyncSource     string   `json:"sync_source,omitempty"`    // 동기화 소스 (holodex, manual)
	ChzzkChannelID string   `json:"chzzkChannelId,omitempty"` // Chzzk 채널 ID
	TwitchUserID   string   `json:"twitchUserId,omitempty"`   // Twitch User ID (immutable)
}

// GetAllAliases: 멤버의 한국어 및 일본어 별명을 모두 합쳐 하나의 슬라이스로 반환합니다.
func (m *Member) GetAllAliases() []string {
	if m.Aliases == nil {
		return []string{}
	}

	all := make([]string, 0, len(m.Aliases.Ko)+len(m.Aliases.Ja))
	all = append(all, m.Aliases.Ko...)
	all = append(all, m.Aliases.Ja...)
	return all
}

// HasAlias: 주어진 이름이 해당 멤버의 별명 목록에 포함되어 있는지 확인합니다.
func (m *Member) HasAlias(name string) bool {
	aliases := m.GetAllAliases()
	return slices.Contains(aliases, name)
}

// GetOrg: org 필드를 반환하고, 빈 값이면 "Hololive"를 기본값으로 반환합니다.
func (m *Member) GetOrg() string {
	if m.Org == "" {
		return "Hololive"
	}
	return m.Org
}

// GetDisplayName: "이름 (그룹)" 형식의 표시 이름을 반환합니다.
func (m *Member) GetDisplayName() string {
	return fmt.Sprintf("%s (%s)", m.Name, m.GetOrg())
}

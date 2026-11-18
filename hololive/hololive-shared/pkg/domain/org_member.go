package domain

import (
	"cmp"
	"fmt"
	"time"
)

type Aliases struct {
	Ko []string `json:"ko"`
	Ja []string `json:"ja"`
}

type Member struct {
	ID              int        `json:"id"`
	ChannelID       string     `json:"channelId"`
	Name            string     `json:"name"`
	Aliases         *Aliases   `json:"aliases,omitempty"`
	NameJa          string     `json:"nameJa,omitempty"`
	NameKo          string     `json:"nameKo,omitempty"`
	ShortKoreanName string     `json:"shortKoreanName,omitempty"`
	IsGraduated     bool       `json:"isGraduated"`
	Photo           string     `json:"photo,omitempty"`
	Units           []string   `json:"units,omitempty"`
	OfficialURL     string     `json:"officialUrl,omitempty"`
	Org             string     `json:"org,omitempty"`
	Suborg          string     `json:"suborg,omitempty"`
	SyncSource      string     `json:"sync_source,omitempty"`
	ChzzkChannelID  string     `json:"chzzkChannelId,omitempty"`
	TwitchUserID    string     `json:"twitchUserId,omitempty"`
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

func (m *Member) GetOrg() string {
	if m.Org == "" {
		return "Hololive"
	}

	return m.Org
}

func (m *Member) DisplayName() string {
	return cmp.Or(m.ShortKoreanName, m.NameKo, m.Name)
}

func (m *Member) QualifiedName() string {
	return fmt.Sprintf("%s (%s)", m.Name, m.GetOrg())
}

package api

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// CreateMemberRequest는 생성 SQL이 저장하는 입력만 받는다. Name은 slug에도 저장된다.
type CreateMemberRequest struct {
	Name            string          `json:"name"`
	ChannelID       string          `json:"channelId"`
	NameJa          string          `json:"nameJa"`
	NameKo          string          `json:"nameKo"`
	ShortKoreanName string          `json:"shortKoreanName"`
	IsGraduated     bool            `json:"isGraduated"`
	Aliases         *domain.Aliases `json:"aliases"`
	Org             string          `json:"org"`
	Units           []string        `json:"units"`
	OfficialURL     string          `json:"officialUrl"`
	Birthday        *time.Time      `json:"birthday"`
	DebutDate       *time.Time      `json:"debutDate"`
	ChzzkChannelID  string          `json:"chzzkChannelId"`
	TwitchUserID    string          `json:"twitchUserId"`
}

func (r CreateMemberRequest) member() (*domain.Member, error) {
	m := &domain.Member{
		Name: strings.TrimSpace(r.Name), ChannelID: strings.TrimSpace(r.ChannelID),
		NameJa: strings.TrimSpace(r.NameJa), NameKo: strings.TrimSpace(r.NameKo),
		ShortKoreanName: strings.TrimSpace(r.ShortKoreanName), IsGraduated: r.IsGraduated,
		Aliases: r.Aliases, Org: strings.TrimSpace(r.Org), Units: r.Units,
		OfficialURL: r.OfficialURL, Birthday: r.Birthday, DebutDate: r.DebutDate,
		ChzzkChannelID: strings.TrimSpace(r.ChzzkChannelID), TwitchUserID: strings.TrimSpace(r.TwitchUserID),
	}
	if m.Name == "" {
		return nil, errors.New("name is required")
	}

	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"name", m.Name, 100},
		{"channelId", m.ChannelID, 64},
		{"nameJa", m.NameJa, 200},
		{"nameKo", m.NameKo, 200},
		{"shortKoreanName", m.ShortKoreanName, 64},
		{"org", m.Org, 50},
		{"chzzkChannelId", m.ChzzkChannelID, 32},
		{"twitchUserId", m.TwitchUserID, 50},
	} {
		if utf8.RuneCountInString(field.value) > field.limit {
			return nil, fmt.Errorf("%s must be at most %d characters", field.name, field.limit)
		}
	}

	return m, nil
}

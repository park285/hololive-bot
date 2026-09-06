// Package mekparkhost는 공유 채널의 제목에서 확인된 진행자와 게스트를 구분한다.
package mekparkhost

import (
	"cmp"
	_ "embed"
	jsonv2 "encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Participant는 제목 근거가 있는 인물과 표시 이름을 담는다.
type Participant struct {
	ID       string
	Name     string
	Evidence []string
}

// Result는 채널에 속한 진행자와 게스트를 구분하며 Hosts가 비면 개인을 특정하지 않는다.
type Result struct {
	Unit   string
	Hosts  []Participant
	Guests []Participant
}

type unitRule struct {
	ChannelID string `json:"channel_id"`
}

type memberRule struct {
	ID           string   `json:"id"`
	Unit         string   `json:"unit"`
	Name         string   `json:"name"`
	FullName     string   `json:"full_name"`
	GivenName    string   `json:"given_name"`
	Tags         []string `json:"tags,omitempty"`
	SeriesTokens []string `json:"series_tokens,omitempty"`
}

type pairRule struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}

type ruleSet struct {
	Version string              `json:"version"`
	Units   map[string]unitRule `json:"units"`
	Members []memberRule        `json:"members"`
	Roles   []string            `json:"roles"`
	Pairs   []pairRule          `json:"pairs"`
}

//go:embed rules.json
var rulesJSON []byte

var loadRules = sync.OnceValue(func() ruleSet {
	var rules ruleSet

	if err := jsonv2.Unmarshal(rulesJSON, &rules, jsonv2.RejectUnknownMembers(true)); err != nil {
		panic(fmt.Errorf("load embedded mekPark host rules: %w", err))
	}

	slices.SortFunc(rules.Members, func(a, b memberRule) int { return cmp.Compare(a.ID, b.ID) })

	return rules
})

var (
	hashSpace = regexp.MustCompile(`#[\s\p{Z}]+`)
	pairSpace = regexp.MustCompile(`[\s\p{Z}]*×[\s\p{Z}]*`)
)

// Identify는 두 mekPark 채널만 제목으로 판별한다. 입력이나 외부 상태를 변경하지 않는다.
func Identify(channelID, title string) Result {
	rules := loadRules()
	result := Result{Unit: rules.unitForChannel(channelID)}

	if result.Unit == "" {
		return result
	}

	title = normalizeTitle(title)

	pairs := rules.pairEvidence(title)
	primary := ""
	participants := make([]Participant, 0, len(rules.Members))

	for i := range rules.Members {
		member := &rules.Members[i]
		evidence, series := member.evidence(title, rules.Roles)

		evidence = append(evidence, pairs[member.ID]...)

		if len(evidence) == 0 {
			continue
		}

		participants = append(participants, Participant{ID: member.ID, Name: member.Name, Evidence: evidence})
		if series && member.Unit == result.Unit {
			primary = member.ID
		}
	}

	for _, participant := range participants {
		member := rules.member(participant.ID)
		if member.Unit == result.Unit && (primary == "" || primary == participant.ID) {
			result.Hosts = append(result.Hosts, participant)
		} else {
			result.Guests = append(result.Guests, participant)
		}
	}

	return result
}

func (r ruleSet) unitForChannel(channelID string) string {
	for id, unit := range r.Units {
		if channelID == unit.ChannelID {
			return id
		}
	}

	return ""
}

func (r ruleSet) member(id string) memberRule {
	for i := range r.Members {
		if r.Members[i].ID == id {
			return r.Members[i]
		}
	}

	return memberRule{}
}

func (m memberRule) evidence(title string, roles []string) (tokens []string, series bool) {
	if strings.Contains(title, m.FullName) {
		tokens = append(tokens, m.FullName)
	}

	for _, tag := range m.Tags {
		if strings.Contains(title, tag) {
			tokens = append(tokens, tag)
		}
	}

	for _, tag := range m.SeriesTokens {
		if strings.Contains(title, tag) {
			tokens = append(tokens, tag)
			series = true
		}
	}

	for _, role := range roles {
		token := m.GivenName + role
		if containsStructuredToken(title, token) {
			tokens = append(tokens, token)
		}
	}

	return tokens, series
}

func (r ruleSet) pairEvidence(title string) map[string][]string {
	title = pairSpace.ReplaceAllString(strings.ReplaceAll(title, "#", ""), "×")

	evidence := make(map[string][]string)

	for _, pair := range r.Pairs {
		token := r.member(pair.Left).GivenName + "×" + r.member(pair.Right).GivenName
		if containsStructuredToken(title, token) {
			evidence[pair.Left] = append(evidence[pair.Left], token)
			evidence[pair.Right] = append(evidence[pair.Right], token)
		}
	}

	return evidence
}

func normalizeTitle(title string) string {
	title = norm.NFKC.String(title)
	title = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		default:
			return r
		}
	}, title)

	return hashSpace.ReplaceAllString(title, "#")
}

func containsStructuredToken(title, token string) bool {
	for {
		before, after, found := strings.Cut(title, token)
		if !found {
			return false
		}

		left, _ := utf8.DecodeLastRuneInString(before)
		right, _ := utf8.DecodeRuneInString(after)
		// 「りらら×ミラの…」는 이름 쌍이며 「りらら×ミラクル」처럼 뒤가 이어진 단어는 제외한다.
		if !unicode.IsLetter(left) && !unicode.IsNumber(left) &&
			(!unicode.IsLetter(right) && !unicode.IsNumber(right) || right == 'の') {
			return true
		}

		title = after
	}
}

// Label은 확인된 진행자와 게스트의 표시를 반환하며, 진행자가 불명확하면 빈 문자열을 반환한다.
func (r Result) Label() string {
	if len(r.Hosts) == 0 {
		return ""
	}

	label := participantNames(r.Hosts)
	if len(r.Guests) > 0 {
		label += " (게스트: " + participantNames(r.Guests) + ")"
	}

	return label
}

func participantNames(participants []Participant) string {
	names := make([]string, 0, len(participants))
	for _, participant := range participants {
		names = append(names, participant.Name)
	}

	return strings.Join(names, " / ")
}

// DisplayName은 기존 채널 표시에 확인된 방송자를 덧붙이며, 미상일 때는 입력 이름을 유지한다.
func DisplayName(channelID, title, channelName string) string {
	label := Identify(channelID, title).Label()
	if label == "" {
		return channelName
	}

	if channelName == "" {
		return label
	}

	return channelName + " · " + label
}

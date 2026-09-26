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
	// 이름+역할 토큰에 바로 붙어도 단어 경계로 인정하는 접두어다.
	RolePrefixes []string   `json:"role_prefixes"`
	Pairs        []pairRule `json:"pairs"`
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

// Identify는 두 mekPark 채널의 제목에서 진행자와 게스트를 판별한다.
// 채널 진행자의 개인 태그·역할·시리즈·합방 표기가 있으면 본문의 성명 언급은 제외한다.
// 입력이나 외부 상태를 변경하지 않으며 근거가 없으면 개인을 특정하지 않는다.
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
	explicitIDs := make(map[string]bool, len(rules.Members))
	hasExplicitHost := false

	for i := range rules.Members {
		member := &rules.Members[i]
		evidence, series, explicit := member.evidence(title, rules.Roles, rules.RolePrefixes)

		evidence = append(evidence, pairs[member.ID]...)
		explicit = explicit || len(pairs[member.ID]) > 0

		if len(evidence) == 0 {
			continue
		}

		participants = append(participants, Participant{ID: member.ID, Name: member.Name, Evidence: evidence})
		explicitIDs[member.ID] = explicit
		hasExplicitHost = hasExplicitHost || (explicit && member.Unit == result.Unit)

		if series && member.Unit == result.Unit {
			primary = member.ID
		}
	}

	for _, participant := range participants {
		// 편지 수신인처럼 본문에서 언급한 이름을 공동 진행자나 게스트로 만들지 않는다.
		if hasExplicitHost && !explicitIDs[participant.ID] {
			continue
		}

		if rules.member(participant.ID).isHost(result.Unit, primary) {
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

func (m memberRule) isHost(unit, primary string) bool {
	// 개인 시리즈에서는 같은 유닛의 다른 출연자도 게스트로 구분한다.
	return m.Unit == unit && (primary == "" || primary == m.ID)
}

func (m memberRule) evidence(title string, roles, rolePrefixes []string) (tokens []string, series, explicit bool) {
	if strings.Contains(title, m.FullName) {
		tokens = append(tokens, m.FullName)
		explicit = containsStructuredToken(title, "#"+m.FullName, true, nil)
	}

	for _, tag := range m.Tags {
		if strings.Contains(title, tag) {
			tokens = append(tokens, tag)
			explicit = true
		}
	}

	for _, tag := range m.SeriesTokens {
		if strings.Contains(title, tag) {
			tokens = append(tokens, tag)
			series = true
			explicit = true
		}
	}

	for _, role := range roles {
		token := m.GivenName + role
		// 「メン限ひなみ視点」처럼 멤버 한정 접두어가 붙은 개인 시점도 같은 역할 근거로 인정한다.
		if containsStructuredToken(title, token, true, rolePrefixes) {
			tokens = append(tokens, token)
			explicit = true
		}
	}

	return tokens, series, explicit
}

func (r ruleSet) pairEvidence(title string) map[string][]string {
	if !strings.Contains(title, "×") {
		return nil
	}

	title = pairSpace.ReplaceAllString(strings.ReplaceAll(title, "#", ""), "×")

	evidence := make(map[string][]string)

	// 성명 사이의 ×는 해시 유무와 무관한 합방 근거다. 「A×Bの曲」의 인용은 제외한다.
	for i := range r.Members {
		left := &r.Members[i]
		for j := range r.Members {
			right := &r.Members[j]
			if left.ID == right.ID {
				continue
			}

			token := left.FullName + "×" + right.FullName
			if containsStructuredToken(title, token, false, nil) {
				evidence[left.ID] = append(evidence[left.ID], token)
				evidence[right.ID] = append(evidence[right.ID], token)
			}
		}
	}

	for _, pair := range r.Pairs {
		token := r.member(pair.Left).GivenName + "×" + r.member(pair.Right).GivenName
		if containsStructuredToken(title, token, true, nil) {
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

func containsStructuredToken(title, token string, allowPossessiveSuffix bool, allowedPrefixes []string) bool {
	// 앞선 일치를 거부한 뒤에도 원래 제목의 앞 문맥으로 경계를 판단한다.
	// 제목을 잘라 내면 「…ミラの枠メン限ミラの枠」의 접두어 앞 문자가 사라져 경계로 오인된다.
	for offset := 0; ; {
		i := strings.Index(title[offset:], token)
		if i < 0 {
			return false
		}

		start := offset + i
		end := start + len(token)

		right, _ := utf8.DecodeRuneInString(title[end:])
		// 「りらら×ミラの…」는 이름 쌍이며 「りらら×ミラクル」처럼 뒤가 이어진 단어는 제외한다.
		if hasLeftBoundary(title[:start], allowedPrefixes) && (!isWordRune(right) || allowPossessiveSuffix && right == 'の') {
			return true
		}

		offset = end
	}
}

// hasLeftBoundary는 토큰 앞이 단어 경계인지 확인한다.
// 허용 접두어는 접두어 앞이 경계일 때만 인정해 「カメラのミラの枠」 같은 단어 중간 일치를 계속 제외한다.
func hasLeftBoundary(before string, allowedPrefixes []string) bool {
	left, _ := utf8.DecodeLastRuneInString(before)
	if !isWordRune(left) {
		return true
	}

	for _, prefix := range allowedPrefixes {
		rest, found := strings.CutSuffix(before, prefix)
		if !found {
			continue
		}

		left, _ = utf8.DecodeLastRuneInString(rest)
		if !isWordRune(left) {
			return true
		}
	}

	return false
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r)
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

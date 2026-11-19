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

type Participant struct {
	ID       string
	Name     string
	Evidence []string
}

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
	Version      string              `json:"version"`
	Units        map[string]unitRule `json:"units"`
	Members      []memberRule        `json:"members"`
	Roles        []string            `json:"roles"`
	RolePrefixes []string            `json:"role_prefixes"`
	Pairs        []pairRule          `json:"pairs"`
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
	for offset := 0; ; {
		i := strings.Index(title[offset:], token)
		if i < 0 {
			return false
		}

		start := offset + i
		end := start + len(token)

		right, _ := utf8.DecodeRuneInString(title[end:])
		if hasLeftBoundary(title[:start], allowedPrefixes) && (!isWordRune(right) || allowPossessiveSuffix && right == 'の') {
			return true
		}

		offset = end
	}
}

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

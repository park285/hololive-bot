package mekparkhost

import (
	"slices"
	"strings"
	"unicode"
)

func SupportsSubscriptions(channelID string) bool {
	return loadRules().unitForChannel(channelID) == "unit-b"
}

func SubscriptionMember(channelID, hostID string) (Participant, bool) {
	if !SupportsSubscriptions(channelID) {
		return Participant{}, false
	}

	member := loadRules().member(hostID)
	if member.Unit != "unit-b" {
		return Participant{}, false
	}

	return Participant{ID: member.ID, Name: subscriptionDisplayName(member.Name)}, true
}

func FindSubscriptionMember(query string) (string, Participant, bool) {
	query = normalizeSubscriptionName(query)

	for _, prefix := range []string{"유닛b", "unitb"} {
		query = strings.TrimPrefix(query, prefix)
	}

	rules := loadRules()
	for i := range rules.Members {
		member := &rules.Members[i]
		if member.Unit != "unit-b" {
			continue
		}

		for _, name := range []string{member.ID, member.Name, member.FullName, member.GivenName} {
			if query == normalizeSubscriptionName(name) {
				return rules.Units[member.Unit].ChannelID, Participant{ID: member.ID, Name: subscriptionDisplayName(member.Name)}, true
			}
		}
	}

	return "", Participant{}, false
}

func subscriptionDisplayName(name string) string {
	return name + "[유닛b]"
}

func normalizeSubscriptionName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}

		return unicode.ToLower(r)
	}, normalizeTitle(value))
}

func (r Result) MatchesSubscription(hostID string) bool {
	if hostID == "" {
		return true
	}

	if r.Unit != "unit-b" || loadRules().member(hostID).Unit != r.Unit {
		return false
	}

	return len(r.Hosts) == 0 || slices.ContainsFunc(r.Hosts, func(host Participant) bool {
		return host.ID == hostID
	})
}

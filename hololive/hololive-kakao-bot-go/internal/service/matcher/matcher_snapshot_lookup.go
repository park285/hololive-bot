package matcher

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func chooseSnapshotString(current, next string) string {
	if current != "" {
		return current
	}

	return next
}

func appendSnapshotEntry(target map[string][]*snapshotEntry, key string, entry *snapshotEntry) {
	if key == "" || entry == nil {
		return
	}

	for _, existing := range target[key] {
		if existing == entry || (existing.candidate != nil && entry.candidate != nil && existing.candidate.channelID == entry.candidate.channelID) {
			return
		}
	}

	target[key] = append(target[key], entry)
}

func splitMemberKey(key string) (name, org string) {
	name = key
	if idx := strings.LastIndex(key, ":"); idx > 0 {
		name = key[:idx]
		org = key[idx+1:]
	}

	return name, org
}

func snapshotTokens(nameNorm string, aliasNorms []string) []string {
	values := make([]string, 0, 1+len(aliasNorms))

	if nameNorm != "" {
		values = append(values, nameNorm)
	}

	values = append(values, aliasNorms...)

	seen := make(map[string]struct{})

	tokens := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}

		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		tokens = append(tokens, value)
	}

	return tokens
}

func pickPreferredSnapshotCandidate(entries []*snapshotEntry) *matchCandidate {
	if len(entries) == 0 {
		return nil
	}

	if len(entries) == 1 {
		return entries[0].candidate
	}

	for _, entry := range entries {
		if entry != nil && entry.candidate != nil && entry.candidate.org == "Hololive" {
			return entry.candidate
		}
	}

	return entries[0].candidate
}

func (mm *MemberMatcher) snapshotMatchStrategies() []snapshotMatchStrategy {
	return []snapshotMatchStrategy{
		{
			name: "exact-alias",
			find: func(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
				if snapshot == nil {
					return nil
				}

				return pickPreferredSnapshotCandidate(snapshot.exactAliases[queryNorm])
			},
		},
		{
			name: "exact-name",
			find: func(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
				if snapshot == nil {
					return nil
				}

				return pickPreferredSnapshotCandidate(snapshot.exactNames[queryNorm])
			},
		},
		{
			name: "partial-name",
			find: func(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
				return mm.trySnapshotPartialNameMatch(snapshot, queryNorm)
			},
		},
		{
			name: "partial-alias",
			find: func(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
				return mm.trySnapshotPartialAliasMatch(snapshot, queryNorm)
			},
		},
	}
}

func cloneCandidateWithStrategy(candidate *matchCandidate, strategy string) *matchCandidate {
	if candidate == nil {
		return nil
	}

	cloned := *candidate

	if strategy == "" {
		return &cloned
	}

	if cloned.source == "" {
		cloned.source = strategy
		return &cloned
	}

	cloned.source = fmt.Sprintf("%s:%s", cloned.source, strategy)

	return &cloned
}

func (mm *MemberMatcher) resolveSnapshotCandidate(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
	for _, strategy := range mm.snapshotMatchStrategies() {
		if strategy.find == nil {
			continue
		}

		if candidate := strategy.find(snapshot, queryNorm); candidate != nil {
			return cloneCandidateWithStrategy(candidate, strategy.name)
		}
	}

	return nil
}

func (mm *MemberMatcher) exactNameMembers(snapshot *memberMatcherSnapshot, nameNorm, org string) []*domain.Member {
	if snapshot == nil || nameNorm == "" {
		return nil
	}

	candidates := make([]*domain.Member, 0, len(snapshot.exactNames[nameNorm]))
	for _, entry := range snapshot.exactNames[nameNorm] {
		member := snapshotMemberForEntry(entry, org)
		if member != nil {
			candidates = append(candidates, member)
		}
	}

	return candidates
}

func snapshotMemberForEntry(entry *snapshotEntry, org string) *domain.Member {
	if entry == nil || entry.candidate == nil {
		return nil
	}

	if org != "" && entry.candidate.org != org {
		return nil
	}

	return &domain.Member{
		Name:      entry.candidate.memberName,
		ChannelID: entry.candidate.channelID,
		Org:       entry.candidate.org,
	}
}

func (mm *MemberMatcher) findSnapshotCandidates(snapshot *memberMatcherSnapshot, queryNorm string) []*snapshotEntry {
	if snapshot == nil || queryNorm == "" {
		return nil
	}

	candidates := snapshot.tokenIndex[queryNorm]
	if len(candidates) > 0 {
		return candidates
	}

	return snapshot.entries
}

func (mm *MemberMatcher) trySnapshotPartialNameMatch(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
	for _, entry := range mm.findSnapshotCandidates(snapshot, queryNorm) {
		if entry == nil || entry.candidate == nil || entry.nameNorm == "" {
			continue
		}

		if strings.Contains(entry.nameNorm, queryNorm) || strings.Contains(queryNorm, entry.nameNorm) {
			return entry.candidate
		}
	}

	return nil
}

func (mm *MemberMatcher) trySnapshotPartialAliasMatch(snapshot *memberMatcherSnapshot, queryNorm string) *matchCandidate {
	for _, entry := range mm.findSnapshotCandidates(snapshot, queryNorm) {
		if entry == nil || entry.candidate == nil {
			continue
		}

		if snapshotAliasesContain(entry.aliasNorms, queryNorm) {
			return entry.candidate
		}
	}

	return nil
}

func snapshotAliasesContain(aliasNorms []string, queryNorm string) bool {
	for _, aliasNorm := range aliasNorms {
		if strings.Contains(aliasNorm, queryNorm) || strings.Contains(queryNorm, aliasNorm) {
			return true
		}
	}

	return false
}

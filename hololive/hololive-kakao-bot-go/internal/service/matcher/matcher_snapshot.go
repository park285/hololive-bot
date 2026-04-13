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

package matcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (mm *MemberMatcher) getSnapshot(ctx context.Context) (*memberMatcherSnapshot, error) {
	mm.snapshotMu.RLock()

	snapshot := mm.snapshot
	mm.snapshotMu.RUnlock()

	if snapshot != nil && time.Since(snapshot.builtAt) < mm.snapshotTTL {
		return snapshot, nil
	}

	value, err, _ := mm.snapshotGroup.Do("member-snapshot", func() (any, error) {
		built, buildErr := mm.buildSnapshot(ctx)
		if buildErr != nil {
			return nil, buildErr
		}

		if built.dynamicLoadErr == nil {
			mm.snapshotMu.Lock()
			mm.snapshot = built
			mm.snapshotMu.Unlock()
		}

		return built, nil
	})
	if err != nil {
		return nil, fmt.Errorf("build member matcher snapshot: %w", err)
	}

	rebuilt, _ := value.(*memberMatcherSnapshot)
	if rebuilt == nil {
		return nil, errors.New("build member matcher snapshot: empty snapshot")
	}

	return rebuilt, nil
}

func (mm *MemberMatcher) buildSnapshot(ctx context.Context) (*memberMatcherSnapshot, error) {
	provider := mm.providerWithContext(ctx)
	snapshot := &memberMatcherSnapshot{
		builtAt:      time.Now(),
		exactNames:   make(map[string][]*snapshotEntry),
		exactAliases: make(map[string][]*snapshotEntry),
		tokenIndex:   make(map[string][]*snapshotEntry),
	}

	if provider == nil {
		return snapshot, nil
	}

	members, err := domain.LoadAllMembers(provider)
	if err != nil {
		return nil, fmt.Errorf("get all members: %w", err)
	}

	entriesByChannel := make(map[string]*snapshotEntry)

	for _, member := range members {
		entry := mm.snapshotEntryFromMember(member)
		if entry == nil {
			continue
		}

		mm.storeSnapshotEntry(snapshot, entriesByChannel, entry)
	}

	if mm.cache != nil {
		dynamicMembers, err := mm.cache.GetAllMembers(ctx)
		if err != nil {
			snapshot.dynamicLoadErr = fmt.Errorf("get all members: %w", err)
		} else {
			for key, channelID := range dynamicMembers {
				entry := mm.snapshotEntryFromDynamic(provider, key, channelID)
				if entry == nil {
					continue
				}

				mm.storeSnapshotEntry(snapshot, entriesByChannel, entry)
			}
		}
	}

	return snapshot, nil
}

func (mm *MemberMatcher) snapshotEntryFromMember(member *domain.Member) *snapshotEntry {
	candidate := mm.candidateFromMember(member, "snapshot")
	if candidate == nil {
		return nil
	}

	entry := &snapshotEntry{
		candidate:  candidate,
		nameNorm:   normalizeMatcherTerm(member.Name),
		aliasNorms: make([]string, 0, len(member.GetAllAliases())+2),
	}
	for _, alias := range member.GetAllAliases() {
		if aliasNorm := normalizeMatcherTerm(alias); aliasNorm != "" {
			entry.aliasNorms = append(entry.aliasNorms, aliasNorm)
		}
	}

	if nameJaNorm := normalizeMatcherTerm(member.NameJa); nameJaNorm != "" {
		entry.aliasNorms = append(entry.aliasNorms, nameJaNorm)
	}

	if nameKoNorm := normalizeMatcherTerm(member.NameKo); nameKoNorm != "" {
		entry.aliasNorms = append(entry.aliasNorms, nameKoNorm)
	}

	if entry.nameNorm == "" {
		entry.nameNorm = normalizeMatcherTerm(candidate.memberName)
	}

	return entry
}

func (mm *MemberMatcher) snapshotEntryFromDynamic(provider domain.MemberDataProvider, key, channelID string) *snapshotEntry {
	name, org := splitMemberKey(key)

	candidate := mm.candidateFromDynamic(provider, name, channelID, "snapshot-dynamic")
	if candidate == nil {
		return nil
	}

	if candidate.org == "" {
		candidate.org = org
	}

	return &snapshotEntry{
		candidate: candidate,
		nameNorm:  normalizeMatcherTerm(name),
	}
}

func (mm *MemberMatcher) storeSnapshotEntry(
	snapshot *memberMatcherSnapshot,
	entriesByChannel map[string]*snapshotEntry,
	entry *snapshotEntry,
) {
	if entry == nil || entry.candidate == nil || entry.candidate.channelID == "" {
		return
	}

	current, exists := entriesByChannel[entry.candidate.channelID]
	if !exists {
		entriesByChannel[entry.candidate.channelID] = entry
		snapshot.entries = append(snapshot.entries, entry)
		current = entry
	}

	if entry.nameNorm != "" {
		current.nameNorm = chooseSnapshotString(current.nameNorm, entry.nameNorm)
		appendSnapshotEntry(snapshot.exactNames, entry.nameNorm, current)
	}

	for _, aliasNorm := range entry.aliasNorms {
		appendSnapshotEntry(snapshot.exactAliases, aliasNorm, current)
	}

	for _, token := range snapshotTokens(entry.nameNorm, entry.aliasNorms) {
		appendSnapshotEntry(snapshot.tokenIndex, token, current)
	}
}

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
		if entry == nil || entry.candidate == nil {
			continue
		}

		if org != "" && entry.candidate.org != org {
			continue
		}

		candidates = append(candidates, &domain.Member{
			Name:      entry.candidate.memberName,
			ChannelID: entry.candidate.channelID,
			Org:       entry.candidate.org,
		})
	}

	return candidates
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

		for _, aliasNorm := range entry.aliasNorms {
			if strings.Contains(aliasNorm, queryNorm) || strings.Contains(queryNorm, aliasNorm) {
				return entry.candidate
			}
		}
	}

	return nil
}

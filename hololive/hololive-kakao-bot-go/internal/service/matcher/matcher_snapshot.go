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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (mm *Matcher) getSnapshot(ctx context.Context) (*matcherSnapshot, error) {
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

	rebuilt, _ := value.(*matcherSnapshot)
	if rebuilt == nil {
		return nil, errors.New("build member matcher snapshot: empty snapshot")
	}

	return rebuilt, nil
}

func (mm *Matcher) buildSnapshot(ctx context.Context) (*matcherSnapshot, error) {
	provider := mm.providerWithContext(ctx)
	snapshot := &matcherSnapshot{
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
	mm.storeSnapshotMembers(snapshot, entriesByChannel, members)
	mm.storeDynamicSnapshotMembers(ctx, provider, snapshot, entriesByChannel)

	return snapshot, nil
}

func (mm *Matcher) storeSnapshotMembers(
	snapshot *matcherSnapshot,
	entriesByChannel map[string]*snapshotEntry,
	members []*domain.Member,
) {
	for _, member := range members {
		entry := mm.snapshotEntryFromMember(member)
		if entry == nil {
			continue
		}

		mm.storeSnapshotEntry(snapshot, entriesByChannel, entry)
	}
}

func (mm *Matcher) storeDynamicSnapshotMembers(
	ctx context.Context,
	provider domain.MemberDataProvider,
	snapshot *matcherSnapshot,
	entriesByChannel map[string]*snapshotEntry,
) {
	if mm.cache == nil {
		return
	}

	dynamicMembers, err := mm.cache.GetAllMembers(ctx)
	if err != nil {
		snapshot.dynamicLoadErr = fmt.Errorf("get all members: %w", err)
		return
	}

	for key, channelID := range dynamicMembers {
		entry := mm.snapshotEntryFromDynamic(provider, key, channelID)
		if entry == nil {
			continue
		}

		mm.storeSnapshotEntry(snapshot, entriesByChannel, entry)
	}
}

func (mm *Matcher) snapshotEntryFromMember(member *domain.Member) *snapshotEntry {
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

func (mm *Matcher) snapshotEntryFromDynamic(provider domain.MemberDataProvider, key, channelID string) *snapshotEntry {
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

func (mm *Matcher) storeSnapshotEntry(
	snapshot *matcherSnapshot,
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

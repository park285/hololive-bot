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

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const matcherSnapshotLoadTimeout = 5 * time.Second

func (mm *Matcher) getSnapshot(ctx context.Context) (*matcherSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	if snapshot := mm.cachedSnapshot(); snapshot != nil {
		return snapshot, nil
	}

	// 첫 요청의 취소는 공유 적재에 전파하지 않는다. 각 대기자는 자기 예산으로 빠지고 적재 자체는 유한 예산을 갖는다.
	sharedCtx := context.WithoutCancel(ctx)
	results := mm.snapshotGroup.DoChan("member-snapshot", func() (any, error) {
		loadCtx, cancel := context.WithTimeout(sharedCtx, matcherSnapshotLoadTimeout)
		defer cancel()

		var loaded *matcherSnapshot

		err := panicguard.RunE(mm.logger, panicguard.BackgroundTask, "matcher-snapshot-load", func() error {
			if cached := mm.cachedSnapshot(); cached != nil {
				loaded = cached
				return nil
			}

			var err error

			loaded, err = mm.rebuildSnapshot(loadCtx)

			return err
		})
		if err != nil {
			return nil, fmt.Errorf("load matcher snapshot: %w", err)
		}

		return loaded, nil
	})

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for member matcher snapshot: %w", ctx.Err())
	case result := <-results:
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("wait for member matcher snapshot: %w", err)
		}

		if result.Err != nil {
			return nil, fmt.Errorf("build member matcher snapshot: %w", result.Err)
		}

		return validatedMatcherSnapshot(result.Val)
	}
}

func (mm *Matcher) cachedSnapshot() *matcherSnapshot {
	mm.snapshotMu.RLock()

	snapshot := mm.snapshot
	mm.snapshotMu.RUnlock()

	if snapshot != nil && time.Since(snapshot.builtAt) < mm.snapshotTTL {
		return snapshot
	}

	return nil
}

func (mm *Matcher) rebuildSnapshot(ctx context.Context) (*matcherSnapshot, error) {
	built, err := mm.buildSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("build snapshot: %w", err)
	}

	mm.snapshotMu.Lock()

	mm.snapshot = built
	mm.snapshotMu.Unlock()

	return built, nil
}

func validatedMatcherSnapshot(value any) (*matcherSnapshot, error) {
	rebuilt, ok := value.(*matcherSnapshot)
	if !ok {
		return nil, fmt.Errorf("build member matcher snapshot: unexpected snapshot type %T", value)
	}

	if rebuilt == nil {
		return nil, errors.New("build member matcher snapshot: empty snapshot")
	}

	return rebuilt, nil
}

func (mm *Matcher) buildSnapshot(ctx context.Context) (*matcherSnapshot, error) {
	provider := mm.membersData
	snapshot := &matcherSnapshot{
		builtAt:      time.Now(),
		exactNames:   make(map[string][]*snapshotEntry),
		exactAliases: make(map[string][]*snapshotEntry),
		tokenIndex:   make(map[string][]*snapshotEntry),
	}

	if provider == nil {
		return snapshot, nil
	}

	members, err := provider.LoadAllMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all members: %w", err)
	}

	snapshot.byChannel = make(map[string]*snapshotEntry, len(members))
	mm.storeSnapshotMembers(snapshot, snapshot.byChannel, members)

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

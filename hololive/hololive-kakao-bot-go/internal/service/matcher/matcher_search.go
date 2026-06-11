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
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (mm *Matcher) maybeCleanupMatchCache() {
	mm.matchCacheMu.Lock()
	defer mm.matchCacheMu.Unlock()

	if time.Since(mm.matchCacheLastCleanup) < mm.matchCacheTTL {
		return
	}

	cutoff := time.Now().Add(-mm.matchCacheTTL)
	for key, entry := range mm.matchCache {
		if entry == nil || entry.Timestamp.Before(cutoff) {
			delete(mm.matchCache, key)
		}
	}

	mm.matchCacheLastCleanup = time.Now()
}

// 캐시된 결과가 있으면 반환하고, 없으면 여러 매칭 전략을 시도한다.
func (mm *Matcher) FindBestMatch(ctx context.Context, query string) (*domain.Channel, error) {
	normalizedQuery := stringutil.Normalize(query)
	cacheKey := fmt.Sprintf("match:%s", normalizedQuery)

	mm.matchCacheMu.RLock()

	cached, found := mm.matchCache[cacheKey]
	mm.matchCacheMu.RUnlock()

	if found {
		age := time.Since(cached.Timestamp)
		if age < mm.matchCacheTTL {
			return cached.Channel, nil
		}

		mm.matchCacheMu.Lock()
		delete(mm.matchCache, cacheKey)
		mm.matchCacheMu.Unlock()
	}

	channel, err := mm.findBestMatchImpl(ctx, query)

	if err == nil {
		mm.storeMatch(cacheKey, channel)
	}

	mm.maybeCleanupMatchCache()

	return channel, err
}

// storeMatch 는 matchCache 에 결과를 저장하되, matchCacheMaxEntries 상한을 강제한다.
// 상한 도달 시 만료 엔트리를 먼저, 없으면 가장 오래된 엔트리를 evict 한다.
func (mm *Matcher) storeMatch(cacheKey string, channel *domain.Channel) {
	now := time.Now()

	mm.matchCacheMu.Lock()
	defer mm.matchCacheMu.Unlock()

	if _, exists := mm.matchCache[cacheKey]; !exists {
		for len(mm.matchCache) >= matchCacheMaxEntries {
			if !mm.evictOneMatchLocked(now) {
				break
			}
		}
	}

	mm.matchCache[cacheKey] = &MatchCacheEntry{
		Channel:   channel,
		Timestamp: now,
	}
}

// evictOneMatchLocked 는 matchCacheMu 보유 상태에서 한 엔트리를 제거한다.
// 만료 엔트리가 있으면 그것을, 없으면 Timestamp 가 가장 오래된 엔트리를 제거한다.
// 제거 성공 시 true 를 반환한다.
func (mm *Matcher) evictOneMatchLocked(now time.Time) bool {
	cutoff := now.Add(-mm.matchCacheTTL)

	var oldestKey string
	var oldestTS time.Time
	hasOldest := false

	for key, entry := range mm.matchCache {
		if entry == nil || entry.Timestamp.Before(cutoff) {
			delete(mm.matchCache, key)
			return true
		}
		if !hasOldest || entry.Timestamp.Before(oldestTS) {
			oldestKey = key
			oldestTS = entry.Timestamp
			hasOldest = true
		}
	}

	if hasOldest {
		delete(mm.matchCache, oldestKey)
		return true
	}
	return false
}

func (mm *Matcher) findBestMatchImpl(ctx context.Context, query string) (*domain.Channel, error) {
	queryNorm := normalizeMatcherTerm(query)

	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	if channel, err := mm.finalizeCandidate(ctx, mm.resolveSnapshotCandidate(snapshot, queryNorm)); err != nil || channel != nil {
		return channel, err
	}

	mm.logger.Debug("No match found in internal data", slog.String("query", query))

	return nil, nil
}

func (mm *Matcher) GetAllMembers() []*domain.Member {
	provider := mm.providerWithContext(mm.ctx)
	if provider == nil {
		return nil
	}

	return provider.GetAllMembers()
}

func (mm *Matcher) GetMemberByChannelID(ctx context.Context, channelID string) *domain.Member {
	provider := mm.providerWithContext(ctx)
	if provider == nil {
		return nil
	}

	return provider.FindMemberByChannelID(channelID)
}

// "이름 (그룹)" 형식을 파싱하고, 동명이인 발생 시 AmbiguousMatchError를 반환합니다.
func (mm *Matcher) FindBestMatchWithCandidates(ctx context.Context, query string) (*domain.Channel, error) {
	name, org := ParseNameWithOrg(query)

	name = mm.normalizeQuery(name)

	nameNorm := normalizeMatcherTerm(name)

	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	if snapshot.dynamicLoadErr != nil {
		return nil, snapshot.dynamicLoadErr
	}

	candidates := mm.exactNameMembers(snapshot, nameNorm, org)

	if len(candidates) == 0 {
		return mm.FindBestMatch(ctx, query)
	}

	if len(candidates) == 1 {
		return mm.memberToChannel(candidates[0]), nil
	}

	if org == "" {
		return nil, NewAmbiguousMatchError(query, candidates)
	}

	return mm.memberToChannel(candidates[0]), nil
}

func (mm *Matcher) memberToChannel(m *domain.Member) *domain.Channel {
	return &domain.Channel{
		ID:   m.ChannelID,
		Name: m.Name,
		Org:  &m.Org,
	}
}

func (mm *Matcher) normalizeQuery(q string) string {
	return strings.TrimSpace(q)
}

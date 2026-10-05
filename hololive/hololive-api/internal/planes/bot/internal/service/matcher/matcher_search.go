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

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
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
// 매칭에 실패하면 (nil, false, nil)을 반환하며, 이 미발견 결과도 캐시에 저장한다.
func (mm *Matcher) FindBestMatch(ctx context.Context, query string) (*domain.Channel, bool, error) {
	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	normalizedQuery := stringutil.Normalize(query)
	cacheKey := fmt.Sprintf("match:%s", normalizedQuery)

	mm.matchCacheMu.RLock()

	cached, cacheHit := mm.matchCache[cacheKey]
	mm.matchCacheMu.RUnlock()

	if cacheHit {
		age := time.Since(cached.Timestamp)
		if cached.snapshot == snapshot && age < mm.matchCacheTTL {
			return cached.Channel, cached.Channel != nil, nil
		}

		mm.matchCacheMu.Lock()
		delete(mm.matchCache, cacheKey)
		mm.matchCacheMu.Unlock()
	}

	channel, found := mm.findBestMatchImpl(snapshot, query)
	mm.storeMatch(cacheKey, channel, snapshot)

	mm.maybeCleanupMatchCache()

	return channel, found, nil
}

// storeMatch 는 matchCache 에 결과를 저장하되, matchCacheMaxEntries 상한을 강제한다.
// 상한 도달 시 만료 엔트리를 먼저, 없으면 가장 오래된 엔트리를 evict 한다.
func (mm *Matcher) storeMatch(cacheKey string, channel *domain.Channel, snapshot *matcherSnapshot) {
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
		snapshot:  snapshot,
	}
}

// evictOneMatchLocked 는 matchCacheMu 보유 상태에서 한 엔트리를 제거한다.
// 만료 엔트리가 있으면 그것을, 없으면 Timestamp 가 가장 오래된 엔트리를 제거한다.
// 제거 성공 시 true 를 반환한다.
func (mm *Matcher) evictOneMatchLocked(now time.Time) bool {
	cutoff := now.Add(-mm.matchCacheTTL)

	var (
		oldestKey string
		oldestTS  time.Time
	)

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

func (mm *Matcher) findBestMatchImpl(snapshot *matcherSnapshot, query string) (*domain.Channel, bool) {
	queryNorm := normalizeMatcherTerm(query)

	channel := mm.finalizeCandidate(mm.resolveSnapshotCandidate(snapshot, queryNorm))
	if channel == nil {
		mm.logger.Debug("No match found in internal data",
			slog.String("query_token", sharedprivacylog.Pseudonym(queryNorm)),
		)
	}

	return channel, channel != nil
}

// GetMemberByChannelID는 채널 대표 멤버를 돌려준다. 멤버 데이터 없이 구성한 Matcher는 snapshot과 같이 빈 데이터로 보고
// domain.ErrMemberNotFound를, 조회 실패는 그 오류를 돌려준다.
func (mm *Matcher) GetMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	if mm == nil || mm.membersData == nil {
		return nil, domain.ErrMemberNotFound
	}

	member, err := mm.membersData.FindMemberByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find member by channel ID: %w", err)
	}

	return member, nil
}

// "이름 (그룹)" 형식을 파싱하고, 동명이인 발생 시 AmbiguousMatchError를 반환합니다.
// 매칭에 실패하면 (nil, false, nil)을 반환합니다.
func (mm *Matcher) FindBestMatchWithCandidates(ctx context.Context, query string) (*domain.Channel, bool, error) {
	name, org := ParseNameWithOrg(query)

	name = mm.normalizeQuery(name)

	nameNorm := normalizeMatcherTerm(name)

	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	candidates := mm.exactNameMembers(snapshot, nameNorm, org)

	if len(candidates) == 0 {
		out, found, err := mm.FindBestMatch(ctx, query)
		if err != nil {
			return nil, false, fmt.Errorf("find best match: %w", err)
		}

		return out, found, nil
	}

	if len(candidates) == 1 {
		return mm.memberToChannel(candidates[0]), true, nil
	}

	if org == "" {
		return nil, false, NewAmbiguousMatchError(query, candidates)
	}

	return mm.memberToChannel(candidates[0]), true, nil
}

func (mm *Matcher) memberToChannel(m *domain.Member) *domain.Channel {
	return &domain.Channel{
		ID:   m.ChannelID,
		Name: m.DisplayName(),
		Org:  &m.Org,
	}
}

// MemberDisplayNames는 channelIDs 중 members에 등록된 채널의 명령 응답 표시명을 돌려준다. 등록되지 않은 채널은 결과에 넣지 않으므로
// 호출자가 원천 응답의 이름을 그대로 쓴다. 멤버 snapshot을 만들지 못하면 오류다.
func (mm *Matcher) MemberDisplayNames(ctx context.Context, channelIDs []string) (map[string]string, error) {
	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	names := make(map[string]string, len(channelIDs))

	for _, channelID := range channelIDs {
		entry := snapshot.byChannel[channelID]
		if entry == nil || entry.candidate == nil {
			continue
		}

		names[channelID] = entry.candidate.member().DisplayName()
	}

	return names, nil
}

func (mm *Matcher) normalizeQuery(q string) string {
	return strings.TrimSpace(q)
}

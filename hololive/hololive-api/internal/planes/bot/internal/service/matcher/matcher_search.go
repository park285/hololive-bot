package matcher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/core/stringutil"

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

func (mm *Matcher) FindBestMatch(ctx context.Context, query string) (*domain.Channel, bool, error) {
	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, false, err
	}

	normalizedQuery := stringutil.Normalize(query)
	cacheKey := "match:" + normalizedQuery

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

	channel, found := mm.findBestMatchImpl(ctx, snapshot, query)
	mm.storeMatch(cacheKey, channel, snapshot)

	mm.maybeCleanupMatchCache()

	return channel, found, nil
}

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

func (mm *Matcher) findBestMatchImpl(ctx context.Context, snapshot *matcherSnapshot, query string) (*domain.Channel, bool) {
	queryNorm := normalizeMatcherTerm(query)

	channel := mm.finalizeCandidate(mm.resolveSnapshotCandidate(snapshot, queryNorm))
	if channel == nil && mm.logger.Enabled(ctx, slog.LevelDebug) {
		mm.logger.Debug("No match found in internal data",
			slog.String("query_token", sharedprivacylog.Pseudonym(queryNorm)),
		)
	}

	return channel, channel != nil
}

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

func (mm *Matcher) FindBestMatchWithCandidates(ctx context.Context, query string) (*domain.Channel, bool, error) {
	member, _, err := mm.FindMemberProfile(ctx, query)
	if err != nil || member == nil {
		return nil, false, err
	}

	return mm.memberToChannel(member), true, nil
}

func (mm *Matcher) FindMemberProfile(ctx context.Context, query string) (*domain.Member, bool, error) {
	name, org := ParseNameWithOrg(query)

	name = mm.normalizeQuery(name)

	nameNorm := normalizeMatcherTerm(name)

	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, false, err
	}

	candidates := mm.exactNameMembers(snapshot, nameNorm, org)

	if len(candidates) == 0 {
		member := mm.resolveSnapshotCandidate(snapshot, normalizeMatcherTerm(query)).member()

		return member, member != nil, nil
	}

	if len(candidates) == 1 {
		return candidates[0], true, nil
	}

	if org == "" {
		return nil, false, NewAmbiguousMatchError(query, candidates)
	}

	return candidates[0], true, nil
}

func (mm *Matcher) memberToChannel(m *domain.Member) *domain.Channel {
	return &domain.Channel{
		ID:   m.ChannelID,
		Name: m.DisplayName(),
		Org:  &m.Org,
	}
}

func (mm *Matcher) MemberDisplayNames(ctx context.Context, channelIDs []string) (map[string]string, error) {
	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, err
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

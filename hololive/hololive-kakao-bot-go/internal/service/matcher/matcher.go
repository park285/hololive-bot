package matcher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"golang.org/x/sync/singleflight"
)

// MatchCacheEntry: 멤버 매칭 결과를 캐싱하기 위한 구조체 (채널 정보 + 타임스탬프)
type MatchCacheEntry struct {
	Channel   *domain.Channel
	Timestamp time.Time
}

type matchCandidate struct {
	channelID  string
	memberName string
	org        string
	source     string
}

type snapshotEntry struct {
	candidate  *matchCandidate
	nameNorm   string
	aliasNorms []string
}

type memberMatcherSnapshot struct {
	builtAt        time.Time
	exactNames     map[string][]*snapshotEntry
	exactAliases   map[string][]*snapshotEntry
	tokenIndex     map[string][]*snapshotEntry
	entries        []*snapshotEntry
	dynamicLoadErr error
}

// ChannelSelector: 모호한 검색어에 대해 모호성 해소를 돕는 채널 선택 인터페이스
type ChannelSelector interface {
	SelectBestChannel(ctx context.Context, query string, candidates []*domain.Channel) (*domain.Channel, error)
}

// MemberMatcher: 사용자 검색어(이름, 별명 등)를 기반으로 Hololive 멤버(채널)를 식별하고 매칭하는 서비스
// 다양한 매칭 전략(정확 일치, 부분 일치, 별명 검색 등)을 순차적으로 시도한다.
type MemberMatcher struct {
	ctx                   context.Context
	membersData           domain.MemberDataProvider
	cache                 cache.Client
	holodex               *holodex.Service
	selector              ChannelSelector
	logger                *slog.Logger
	matchCache            map[string]*MatchCacheEntry
	matchCacheMu          sync.RWMutex
	matchCacheTTL         time.Duration
	matchCacheLastCleanup time.Time
	snapshotMu            sync.RWMutex
	snapshot              *memberMatcherSnapshot
	snapshotTTL           time.Duration
	snapshotGroup         singleflight.Group
}

// NewMemberMatcher: 새로운 MemberMatcher 인스턴스를 생성합니다.
func NewMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	selector ChannelSelector,
	logger *slog.Logger,
) *MemberMatcher {
	mm := &MemberMatcher{
		ctx:                   ctx,
		membersData:           membersData,
		cache:                 cacheSvc,
		holodex:               holodexSvc,
		selector:              selector,
		logger:                logger,
		matchCache:            make(map[string]*MatchCacheEntry),
		matchCacheTTL:         1 * time.Minute,
		matchCacheLastCleanup: time.Now(),
		snapshotTTL:           1 * time.Minute,
	}

	provider := mm.providerWithContext(ctx)
	memberCount := 0
	if provider != nil {
		memberCount = len(provider.GetAllMembers())
	}

	logger.Info("MemberMatcher initialized",
		slog.Int("members", memberCount),
	)

	return mm
}

func (mm *MemberMatcher) providerWithContext(ctx context.Context) domain.MemberDataProvider {
	if mm == nil || mm.membersData == nil {
		return nil
	}
	if ctx == nil {
		return mm.membersData
	}
	return mm.membersData.WithContext(ctx)
}

// tryExactValkeyMatch: 동적 Valkey 데이터에서 정확한 매칭을 시도함 (Holodex 호출 없이)
func (mm *MemberMatcher) tryExactValkeyMatch(provider domain.MemberDataProvider, query string, dynamicMembers map[string]string) *matchCandidate {
	var candidates []*matchCandidate
	for name, channelID := range dynamicMembers {
		if strings.EqualFold(name, query) {
			candidates = append(candidates, mm.candidateFromDynamic(provider, name, channelID, "valkey-exact"))
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0]
	}

	for _, c := range candidates {
		if provider != nil {
			if member := provider.FindMemberByChannelID(c.channelID); member != nil {
				if member.Org == "Hololive" {
					return c
				}
			}
		}
	}

	return candidates[0]
}

// tryPartialStaticMatch: 정적 멤버 데이터에서 부분 매칭을 시도함
func (mm *MemberMatcher) tryPartialStaticMatch(provider domain.MemberDataProvider, queryNorm string) *matchCandidate {
	if provider != nil {
		for _, member := range provider.GetAllMembers() {
			nameNorm := stringutil.Normalize(member.Name)
			if strings.Contains(nameNorm, queryNorm) || strings.Contains(queryNorm, nameNorm) {
				return mm.candidateFromMember(member, "static-partial")
			}
		}
	}

	return nil
}

// tryPartialValkeyMatch: 동적 Valkey 데이터에서 부분 매칭을 시도함
func (mm *MemberMatcher) tryPartialValkeyMatch(provider domain.MemberDataProvider, queryNorm string, dynamicMembers map[string]string) *matchCandidate {
	for name, channelID := range dynamicMembers {
		nameNorm := stringutil.Normalize(name)
		if strings.Contains(nameNorm, queryNorm) || strings.Contains(queryNorm, nameNorm) {
			return mm.candidateFromDynamic(provider, name, channelID, "valkey-partial")
		}
	}
	return nil
}

// tryPartialAliasMatch: 모든 별칭에서 부분 매칭을 시도함
func (mm *MemberMatcher) tryPartialAliasMatch(provider domain.MemberDataProvider, queryNorm string) *matchCandidate {
	if provider != nil {
		for _, member := range provider.GetAllMembers() {
			for _, alias := range member.GetAllAliases() {
				aliasNorm := stringutil.Normalize(alias)
				if strings.Contains(aliasNorm, queryNorm) || strings.Contains(queryNorm, aliasNorm) {
					return mm.candidateFromMember(member, "alias-partial")
				}
			}
		}
	}

	return nil
}

func (mm *MemberMatcher) candidateFromMember(member *domain.Member, source string) *matchCandidate {
	if member == nil || member.ChannelID == "" {
		return nil
	}

	name := member.Name
	if name == "" {
		name = member.NameJa
	}
	if name == "" {
		name = member.ChannelID
	}

	return &matchCandidate{
		channelID:  member.ChannelID,
		memberName: name,
		org:        member.GetOrg(),
		source:     source,
	}
}

func (mm *MemberMatcher) candidateFromDynamic(provider domain.MemberDataProvider, name, channelID, source string) *matchCandidate {
	if channelID == "" {
		return nil
	}

	if provider != nil {
		if member := provider.FindMemberByChannelID(channelID); member != nil {
			if candidate := mm.candidateFromMember(member, source); candidate != nil {
				return candidate
			}
		}
	}

	displayName := name
	if displayName == "" {
		displayName = channelID
	}

	return &matchCandidate{
		channelID:  channelID,
		memberName: displayName,
		org:        "",
		source:     source,
	}
}

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
		return nil, fmt.Errorf("build member matcher snapshot: empty snapshot")
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

	entriesByChannel := make(map[string]*snapshotEntry)
	for _, member := range provider.GetAllMembers() {
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

func normalizeMatcherTerm(value string) string {
	return util.NormalizeSuffix(value)
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

func (mm *MemberMatcher) hydrateChannel(ctx context.Context, candidate *matchCandidate) (*domain.Channel, error) {
	if candidate == nil {
		return nil, nil
	}

	fallback := &domain.Channel{
		ID:   candidate.channelID,
		Name: candidate.memberName,
	}
	if candidate.memberName != "" {
		fallback.EnglishName = toStringPtr(candidate.memberName)
	}

	if mm.holodex == nil {
		return fallback, nil
	}

	channel, err := mm.holodex.GetChannel(ctx, candidate.channelID)
	if err != nil {
		mm.logger.Warn("Failed to fetch channel from Holodex",
			slog.String("channel_id", candidate.channelID),
			slog.String("source", candidate.source),
			slog.Any("error", err),
		)
		// Holodex 실패 시 캐시에서 채널명 조회 시도
		if mm.cache != nil {
			if cachedName, cacheErr := mm.cache.HGet(ctx, constants.RedisKeys.AlarmMemberNames, candidate.channelID); cacheErr == nil && cachedName != "" {
				fallback.Name = cachedName
				mm.logger.Debug("Using cached channel name as fallback",
					slog.String("channel_id", candidate.channelID),
					slog.String("cached_name", cachedName),
				)
			}
		}
		return fallback, nil
	}

	if channel == nil {
		mm.logger.Warn("Holodex returned empty channel",
			slog.String("channel_id", candidate.channelID),
			slog.String("source", candidate.source),
		)
		return fallback, nil
	}

	if candidate.memberName != "" {
		if channel.Name == "" {
			channel.Name = candidate.memberName
		}
		if channel.EnglishName == nil {
			channel.EnglishName = toStringPtr(candidate.memberName)
		}
	}

	return channel, nil
}

func (mm *MemberMatcher) finalizeCandidate(ctx context.Context, candidate *matchCandidate) (*domain.Channel, error) {
	if candidate == nil {
		return nil, nil
	}

	if candidate.channelID == "" {
		mm.logger.Warn("Match candidate missing channel ID",
			slog.String("member", candidate.memberName),
			slog.String("source", candidate.source),
		)
		return nil, nil
	}

	channel, err := mm.hydrateChannel(ctx, candidate)
	if err != nil {
		return nil, err
	}

	if channel != nil {
		mm.logger.Debug("Match candidate resolved",
			slog.String("channel_id", candidate.channelID),
			slog.String("member", candidate.memberName),
			slog.String("source", candidate.source),
		)
	}

	return channel, nil
}

func (mm *MemberMatcher) maybeCleanupMatchCache() {
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

func toStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copied := value
	return new(copied)
}

// loadDynamicMembers: Valkey 캐시에서 멤버 데이터를 로드함
func (mm *MemberMatcher) loadDynamicMembers(ctx context.Context) map[string]string {
	members, err := mm.cache.GetAllMembers(ctx)
	if err != nil {
		mm.logger.Warn("Failed to load dynamic members", slog.Any("error", err))
		return map[string]string{}
	}
	return members
}

// FindBestMatch: 주어진 쿼리 문자열과 가장 잘 일치하는 멤버/채널을 찾는다.
// 캐시된 결과가 있으면 반환하고, 없으면 여러 매칭 전략을 시도한다.
func (mm *MemberMatcher) FindBestMatch(ctx context.Context, query string) (*domain.Channel, error) {
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

	mm.matchCacheMu.Lock()
	mm.matchCache[cacheKey] = &MatchCacheEntry{
		Channel:   channel,
		Timestamp: time.Now(),
	}
	mm.matchCacheMu.Unlock()

	mm.maybeCleanupMatchCache()

	return channel, err
}

func (mm *MemberMatcher) findBestMatchImpl(ctx context.Context, query string) (*domain.Channel, error) {
	queryNorm := util.NormalizeSuffix(query)
	snapshot, err := mm.getSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("get member matcher snapshot: %w", err)
	}

	// Strategy 1: 정확한 별칭 매칭 (가장 빠름)
	if channel, err := mm.finalizeCandidate(ctx, pickPreferredSnapshotCandidate(snapshot.exactAliases[queryNorm])); err != nil || channel != nil {
		return channel, err
	}

	// Strategy 2: exact name snapshot
	if channel, err := mm.finalizeCandidate(ctx, pickPreferredSnapshotCandidate(snapshot.exactNames[queryNorm])); err != nil || channel != nil {
		return channel, err
	}

	// Strategy 3: snapshot token/name 부분 매칭
	if channel, err := mm.finalizeCandidate(ctx, mm.trySnapshotPartialNameMatch(snapshot, queryNorm)); err != nil || channel != nil {
		return channel, err
	}

	// Strategy 4: snapshot alias 부분 매칭
	if channel, err := mm.finalizeCandidate(ctx, mm.trySnapshotPartialAliasMatch(snapshot, queryNorm)); err != nil || channel != nil {
		return channel, err
	}

	// 내부 데이터에서 매칭 실패 - nil 반환하여 상위에서 "멤버를 찾을 수 없습니다" 오류 표시
	mm.logger.Debug("No match found in internal data", slog.String("query", query))
	return nil, nil
}

// GetAllMembers: 등록된 모든 멤버 정보를 반환합니다.
func (mm *MemberMatcher) GetAllMembers() []*domain.Member {
	provider := mm.providerWithContext(mm.ctx)
	if provider == nil {
		return nil
	}
	return provider.GetAllMembers()
}

// GetMemberByChannelID: 채널 ID를 사용하여 멤버 정보를 조회합니다.
func (mm *MemberMatcher) GetMemberByChannelID(ctx context.Context, channelID string) *domain.Member {
	provider := mm.providerWithContext(ctx)
	if provider == nil {
		return nil
	}
	return provider.FindMemberByChannelID(channelID)
}

// FindBestMatchWithCandidates: !알람 명령어 전용 매칭 API.
// "이름 (그룹)" 형식을 파싱하고, 동명이인 발생 시 AmbiguousMatchError를 반환합니다.
func (mm *MemberMatcher) FindBestMatchWithCandidates(ctx context.Context, query string) (*domain.Channel, error) {
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

	var candidates []*domain.Member
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

func (mm *MemberMatcher) memberToChannel(m *domain.Member) *domain.Channel {
	return &domain.Channel{
		ID:   m.ChannelID,
		Name: m.Name,
		Org:  &m.Org,
	}
}

func (mm *MemberMatcher) normalizeQuery(q string) string {
	return strings.TrimSpace(q)
}

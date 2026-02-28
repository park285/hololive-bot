package member

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	memberChannelKeyPrefix = "member:channel:"
	memberNameKeyPrefix    = "member:name:"
	memberAliasKeyPrefix   = "member:alias:"
	memberCachePattern     = "member:*"
	allChannelIDsKey       = "channel_ids"
)

// Cache: 멤버 정보를 인메모리(Local) 및 Redis(Valkey)에 이중 캐싱하여 관리하는 서비스
// DB 부하를 줄이고 빠른 조회를 지원하며, 워밍업(Warm-up) 기능을 제공합니다.
type Cache struct {
	repo   *Repository
	cache  *cache.Service
	logger *slog.Logger

	// 인메모리 캐시
	byChannelID sync.Map // map[string]*domain.Member
	byName      sync.Map // map[string]*domain.Member
	allMembers  sync.Map // []string (channel IDs)

	// 캐시 설정
	cacheTTL time.Duration
	warmup   bool

	// 워밍업 설정
	warmUpChunkSize     int
	warmUpMaxGoroutines int
}

// CacheConfig: 멤버 캐시 설정을 위한 구조체 (TTL, 워밍업 옵션 등)
type CacheConfig struct {
	ValkeyTTL           time.Duration
	WarmUp              bool // 시작 시 전체 멤버를 메모리에 로드
	WarmUpChunkSize     int
	WarmUpMaxGoroutines int
}

// NewMemberCache: 새로운 멤버 캐시 서비스 인스턴스를 생성하고 초기화합니다.
// 설정에 따라 생성 시점에 자동으로 캐시 워밍업을 수행할 수 있다.
func NewMemberCache(ctx context.Context, repo *Repository, cacheService *cache.Service, logger *slog.Logger, cfg CacheConfig) (*Cache, error) {
	if cfg.ValkeyTTL == 0 {
		cfg.ValkeyTTL = constants.MemberCacheDefaults.ValkeyTTL
	}
	if cfg.WarmUpChunkSize == 0 {
		cfg.WarmUpChunkSize = constants.MemberCacheDefaults.WarmUpChunkSize
	}
	if cfg.WarmUpMaxGoroutines == 0 {
		cfg.WarmUpMaxGoroutines = constants.MemberCacheDefaults.WarmUpMaxGoroutines
	}

	mc := &Cache{
		repo:     repo,
		cache:    cacheService,
		logger:   logger,
		cacheTTL: cfg.ValkeyTTL,
		warmup:   cfg.WarmUp,

		warmUpChunkSize:     cfg.WarmUpChunkSize,
		warmUpMaxGoroutines: cfg.WarmUpMaxGoroutines,
	}

	// 워밍업이 활성화된 경우 캐시 워밍업 수행함
	if cfg.WarmUp {
		if err := mc.WarmUpCache(ctx); err != nil {
			logger.Warn("Failed to warm up member cache", slog.Any("error", err))
		}
	}

	return mc, nil
}

func (c *Cache) cacheEnabled() bool {
	return c != nil && c.cache != nil
}

// WarmUpCache: DB에서 모든 멤버 정보를 로드하여 Redis 및 인메모리 캐시에 적재합니다.
// 병렬 처리를 통해 대량의 데이터도 빠르게 처리한다.
func (c *Cache) WarmUpCache(ctx context.Context) error {
	members, err := c.repo.GetAllMembers(ctx)
	if err != nil {
		return fmt.Errorf("failed to load all members: %w", err)
	}

	// 병렬 처리를 위한 청크 분할
	chunkSize := c.warmUpChunkSize
	chunks := chunkMembers(members, chunkSize)

	// Worker pool로 병렬 캐싱
	p := pool.New().WithMaxGoroutines(c.warmUpMaxGoroutines)
	for _, chunk := range chunks {
		p.Go(func() {
			c.cacheChunk(ctx, chunk)
		})
	}
	p.Wait()

	// 인메모리 캐시 업데이트
	for _, member := range members {
		if member.ChannelID != "" {
			c.byChannelID.Store(member.ChannelID, member)
		}
		c.byName.Store(member.Name, member)
	}

	c.logger.Info("Member cache warmed up",
		slog.Int("total_members", len(members)),
		slog.Int("chunks", len(chunks)),
	)

	return nil
}

// 청크 단위로 Valkey에 파이프라이닝 저장
func (c *Cache) cacheChunk(ctx context.Context, members []*domain.Member) {
	if len(members) == 0 {
		return
	}
	if !c.cacheEnabled() {
		return
	}

	// 배치 저장을 위한 맵 준비
	pairs := make(map[string]any, len(members)*2)

	for _, member := range members {
		if member.ChannelID != "" {
			channelKey := memberChannelKeyPrefix + member.ChannelID
			pairs[channelKey] = member
		}

		nameKey := memberNameKeyPrefix + member.Name
		pairs[nameKey] = member
	}

	// MSet으로 배치 저장
	if err := c.cache.MSet(ctx, pairs, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to batch cache members",
			slog.Int("count", len(members)),
			slog.Any("error", err))
	}
}

// GetByChannelID: 채널 ID로 멤버 정보를 조회한다. (1. 인메모리 -> 2. Redis -> 3. DB 순서로 확인)
func (c *Cache) GetByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	// 인메모리 캐시 먼저 확인
	if val, ok := c.byChannelID.Load(channelID); ok {
		return val.(*domain.Member), nil
	}

	if c.cacheEnabled() {
		// Valkey 캐시 확인
		cacheKey := memberChannelKeyPrefix + channelID
		var member domain.Member
		if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
			// 인메모리 캐시에 저장
			c.byChannelID.Store(channelID, &member)
			return &member, nil
		}
	}

	// DB 조회
	dbMember, err := c.repo.FindByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	// 캐시 저장
	c.cacheMember(ctx, dbMember)

	return dbMember, nil
}

// GetByName: 멤버 이름으로 정보를 조회한다. (1. 인메모리 -> 2. Redis -> 3. DB 순서로 확인)
func (c *Cache) GetByName(ctx context.Context, name string) (*domain.Member, error) {
	// 인메모리 캐시 먼저 확인
	if val, ok := c.byName.Load(name); ok {
		return val.(*domain.Member), nil
	}

	if c.cacheEnabled() {
		// Valkey 캐시 확인
		cacheKey := memberNameKeyPrefix + name
		var member domain.Member
		if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
			c.byName.Store(name, &member)
			return &member, nil
		}
	}

	// DB 조회
	dbMember, err := c.repo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	c.cacheMember(ctx, dbMember)
	return dbMember, nil
}

// FindByAlias: 멤버 별명으로 정보를 조회한다. (Redis -> DB 순서)
// 별명 조회 성공 시 해당 멤버 정보를 캐시에 등록한다.
func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if c.cacheEnabled() {
		// Valkey 캐시 확인
		cacheKey := memberAliasKeyPrefix + alias
		var member domain.Member
		if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
			// 인메모리 캐시에도 저장
			if member.ChannelID != "" {
				c.byChannelID.Store(member.ChannelID, &member)
			}
			c.byName.Store(member.Name, &member)
			return &member, nil
		}
	}

	// DB 조회
	dbMember, err := c.repo.FindByAlias(ctx, alias)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	// 캐시 저장
	c.cacheMember(ctx, dbMember)

	if c.cacheEnabled() {
		// Alias 키도 캐싱
		cacheKey := memberAliasKeyPrefix + alias
		_ = c.cache.Set(ctx, cacheKey, dbMember, c.cacheTTL)
	}

	return dbMember, nil
}

// GetAllChannelIDs: 모든 멤버의 채널 ID 목록을 반환한다. (인메모리 캐싱됨)
func (c *Cache) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	if val, ok := c.allMembers.Load(allChannelIDsKey); ok {
		return val.([]string), nil
	}

	channelIDs, err := c.repo.GetAllChannelIDs(ctx)
	if err != nil {
		return nil, err
	}

	c.allMembers.Store(allChannelIDsKey, channelIDs)

	return channelIDs, nil
}

func (c *Cache) cacheMember(ctx context.Context, member *domain.Member) {
	if member.ChannelID != "" {
		c.byChannelID.Store(member.ChannelID, member)
	}
	c.byName.Store(member.Name, member)

	if !c.cacheEnabled() {
		return
	}

	// Valkey에도 저장
	if member.ChannelID != "" {
		channelKey := memberChannelKeyPrefix + member.ChannelID
		if err := c.cache.Set(ctx, channelKey, member, c.cacheTTL); err != nil {
			c.logger.Warn("Failed to cache member by channel ID",
				slog.String("channel_id", member.ChannelID),
				slog.Any("error", err),
			)
		}
	}

	nameKey := memberNameKeyPrefix + member.Name
	if err := c.cache.Set(ctx, nameKey, member, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to cache member by name",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)
	}
}

// InvalidateAll: 모든 멤버 관련 캐시(인메모리 및 Redis)를 무효화(삭제)합니다.
func (c *Cache) InvalidateAll(ctx context.Context) error {
	// 인메모리 캐시 클리어
	c.byChannelID = sync.Map{}
	c.byName = sync.Map{}
	c.allMembers = sync.Map{}

	if !c.cacheEnabled() {
		c.logger.Info("Member cache invalidated", slog.Int("keys_deleted", 0))
		return nil
	}

	// Valkey 캐시 클리어 (SCAN 사용으로 Redis 블로킹 방지)
	keys, err := c.cache.ScanKeys(ctx, memberCachePattern, 100)
	if err != nil {
		return fmt.Errorf("failed to scan keys for invalidation: %w", err)
	}
	if len(keys) > 0 {
		if _, err := c.cache.DelMany(ctx, keys); err != nil {
			return fmt.Errorf("failed to invalidate cache store: %w", err)
		}
	}

	c.logger.Info("Member cache invalidated", slog.Int("keys_deleted", len(keys)))
	return nil
}

// Refresh: 캐시를 전체 무효화한 후 다시 워밍업을 수행합니다.
func (c *Cache) Refresh(ctx context.Context) error {
	if err := c.InvalidateAll(ctx); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}
	return c.WarmUpCache(ctx)
}

// InvalidateAliasCache: 특정 별칭에 대한 Valkey 캐시를 무효화합니다.
func (c *Cache) InvalidateAliasCache(ctx context.Context, alias string) error {
	if !c.cacheEnabled() {
		c.logger.Info("Alias cache invalidated", slog.String("alias", alias))
		return nil
	}

	aliasKey := memberAliasKeyPrefix + alias
	if err := c.cache.Del(ctx, aliasKey); err != nil {
		c.logger.Warn("Failed to invalidate alias cache",
			slog.String("alias", alias),
			slog.Any("error", err),
		)
		return fmt.Errorf("failed to invalidate alias cache: %w", err)
	}

	c.logger.Info("Alias cache invalidated", slog.String("alias", alias))
	return nil
}

// 청크 분할 헬퍼
func chunkMembers(members []*domain.Member, chunkSize int) [][]*domain.Member {
	var chunks [][]*domain.Member
	for i := 0; i < len(members); i += chunkSize {
		end := min(i+chunkSize, len(members))
		chunks = append(chunks, members[i:end])
	}
	return chunks
}

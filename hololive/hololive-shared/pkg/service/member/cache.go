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

package member

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

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

// DB 부하를 줄이고 빠른 조회를 지원하며, 워밍업(Warm-up) 기능을 제공합니다.
type Cache struct {
	repo   *Repository
	cache  cache.Client
	logger *slog.Logger

	byChannelID sync.Map // map[string]*domain.Member
	byName      sync.Map // map[string]*domain.Member
	allMembers  sync.Map // []string (channel IDs)

	cacheTTL time.Duration
	warmup   bool

	warmUpChunkSize     int
	warmUpMaxGoroutines int
}

type CacheConfig struct {
	ValkeyTTL           time.Duration
	WarmUp              bool // 시작 시 전체 멤버를 메모리에 로드
	WarmUpChunkSize     int
	WarmUpMaxGoroutines int
}

// 설정에 따라 생성 시점에 자동으로 캐시 워밍업을 수행할 수 있다.
func NewMemberCache(ctx context.Context, repo *Repository, cacheService cache.Client, logger *slog.Logger, cfg CacheConfig) (*Cache, error) {
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

// 병렬 처리를 통해 대량의 데이터도 빠르게 처리한다.
func (c *Cache) WarmUpCache(ctx context.Context) error {
	members, err := c.repo.GetAllMembers(ctx)
	if err != nil {
		return fmt.Errorf("failed to load all members: %w", err)
	}

	chunkSize := c.warmUpChunkSize
	chunks := chunkMembers(members, chunkSize)

	maxWorkers := max(1, c.warmUpMaxGoroutines)
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, chunk := range chunks {
		wg.Go(func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			c.cacheChunk(ctx, chunk)
		})
	}
	wg.Wait()

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

func (c *Cache) cacheChunk(ctx context.Context, members []*domain.Member) {
	if len(members) == 0 {
		return
	}
	if !c.cacheEnabled() {
		return
	}

	pairs := make(map[string]any, len(members)*2)

	for _, member := range members {
		if member.ChannelID != "" {
			channelKey := memberChannelKeyPrefix + member.ChannelID
			pairs[channelKey] = member
		}

		nameKey := memberNameKeyPrefix + member.Name
		pairs[nameKey] = member
	}

	if err := c.cache.MSet(ctx, pairs, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to batch cache members",
			slog.Int("count", len(members)),
			slog.Any("error", err))
	}
}

func (c *Cache) GetByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	if val, ok := c.byChannelID.Load(channelID); ok {
		return val.(*domain.Member), nil
	}

	if c.cacheEnabled() {
		cacheKey := memberChannelKeyPrefix + channelID
		var member domain.Member
		if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
			c.byChannelID.Store(channelID, &member)
			return &member, nil
		}
	}

	dbMember, err := c.repo.FindByChannelID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	c.cacheMember(ctx, dbMember)

	return dbMember, nil
}

func (c *Cache) GetByName(ctx context.Context, name string) (*domain.Member, error) {
	if val, ok := c.byName.Load(name); ok {
		return val.(*domain.Member), nil
	}

	if c.cacheEnabled() {
		cacheKey := memberNameKeyPrefix + name
		var member domain.Member
		if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
			c.byName.Store(name, &member)
			return &member, nil
		}
	}

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

// 별명 조회 성공 시 해당 멤버 정보를 캐시에 등록한다.
func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if member := c.getAliasFromCache(ctx, alias); member != nil {
		return member, nil
	}

	dbMember, err := c.repo.FindByAlias(ctx, alias)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	c.cacheMember(ctx, dbMember)

	if c.cacheEnabled() {
		cacheKey := memberAliasKeyPrefix + alias
		_ = c.cache.Set(ctx, cacheKey, dbMember, c.cacheTTL)
	}

	return dbMember, nil
}

func (c *Cache) getAliasFromCache(ctx context.Context, alias string) *domain.Member {
	if !c.cacheEnabled() {
		return nil
	}
	cacheKey := memberAliasKeyPrefix + alias
	var member domain.Member
	if err := c.cache.Get(ctx, cacheKey, &member); err != nil || member.Name == "" {
		return nil
	}
	if member.ChannelID != "" {
		c.byChannelID.Store(member.ChannelID, &member)
	}
	c.byName.Store(member.Name, &member)
	return &member
}

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

func (c *Cache) Refresh(ctx context.Context) error {
	if err := c.InvalidateAll(ctx); err != nil {
		return fmt.Errorf("failed to invalidate cache: %w", err)
	}
	return c.WarmUpCache(ctx)
}

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

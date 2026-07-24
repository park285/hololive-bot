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
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

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
	allMembersSnapshotKey  = "all_members"
)

// DB 부하를 줄이고 빠른 조회를 지원하며, 워밍업(Warm-up) 기능을 제공합니다.
type Cache struct {
	repository *Repository
	cache      cache.KeyValueCache
	logger     *slog.Logger

	byChannelID sync.Map // map[string]*domain.Member
	byName      sync.Map // map[string]*domain.Member
	allMembers  sync.Map // []string (channel IDs)

	allMembersSnapshot atomic.Pointer[allMembersState]
	allMembersGroup    singleflight.Group
	snapshotTTL        time.Duration
	loadAllMembers     func(ctx context.Context) ([]*domain.Member, error)

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
func NewMemberCache(ctx context.Context, repository *Repository, cacheService cache.KeyValueCache, logger *slog.Logger, config CacheConfig) (*Cache, error) {
	if config.ValkeyTTL == 0 {
		config.ValkeyTTL = constants.MemberCacheDefaults.ValkeyTTL
	}
	if config.WarmUpChunkSize == 0 {
		config.WarmUpChunkSize = constants.MemberCacheDefaults.WarmUpChunkSize
	}
	if config.WarmUpMaxGoroutines == 0 {
		config.WarmUpMaxGoroutines = constants.MemberCacheDefaults.WarmUpMaxGoroutines
	}

	mc := &Cache{
		repository:  repository,
		cache:       cacheService,
		logger:      logger,
		cacheTTL:    config.ValkeyTTL,
		warmup:      config.WarmUp,
		snapshotTTL: allMembersSnapshotTTL,

		warmUpChunkSize:     config.WarmUpChunkSize,
		warmUpMaxGoroutines: config.WarmUpMaxGoroutines,
	}

	if config.WarmUp {
		if err := mc.WarmUpCache(ctx); err != nil {
			logger.Warn("Failed to warm up member cache", slog.Any("error", err))
		}
	}

	return mc, nil
}

func (c *Cache) cacheEnabled() bool {
	return c != nil && c.cache != nil
}

func (c *Cache) GetByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	if member, ok := c.loadChannelFromMemory(channelID); ok {
		return member, nil
	}

	if member := c.loadChannelFromDistributedCache(ctx, channelID); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByChannelID(ctx, channelID)
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
	if member, ok := c.loadNameFromMemory(name); ok {
		return member, nil
	}

	if member := c.loadNameFromDistributedCache(ctx, name); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	c.cacheMember(ctx, dbMember)
	return dbMember, nil
}

func (c *Cache) loadChannelFromMemory(channelID string) (*domain.Member, bool) {
	val, ok := c.byChannelID.Load(channelID)
	if !ok {
		return nil, false
	}
	member, ok := val.(*domain.Member)
	if ok {
		return member, true
	}
	c.byChannelID.Delete(channelID)
	return nil, false
}

func (c *Cache) loadNameFromMemory(name string) (*domain.Member, bool) {
	val, ok := c.byName.Load(name)
	if !ok {
		return nil, false
	}
	member, ok := val.(*domain.Member)
	if ok {
		return member, true
	}
	c.byName.Delete(name)
	return nil, false
}

func (c *Cache) loadChannelFromDistributedCache(ctx context.Context, channelID string) *domain.Member {
	if !c.cacheEnabled() {
		return nil
	}
	cacheKey := memberChannelKeyPrefix + channelID
	var member domain.Member
	if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
		c.byChannelID.Store(channelID, &member)
		return &member
	}
	return nil
}

func (c *Cache) loadNameFromDistributedCache(ctx context.Context, name string) *domain.Member {
	if !c.cacheEnabled() {
		return nil
	}
	cacheKey := memberNameKeyPrefix + name
	var member domain.Member
	if err := c.cache.Get(ctx, cacheKey, &member); err == nil && member.Name != "" {
		c.byName.Store(name, &member)
		return &member
	}
	return nil
}

// 별명 조회 성공 시 해당 멤버 정보를 캐시에 등록한다.
func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if member := c.getAliasFromCache(ctx, alias); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByAlias(ctx, alias)
	if err != nil {
		return nil, err
	}
	if dbMember == nil {
		return nil, nil
	}

	c.cacheMember(ctx, dbMember)

	if c.cacheEnabled() {
		cacheKey := memberAliasKeyPrefix + alias
		if err := c.cache.Set(ctx, cacheKey, dbMember, c.cacheTTL); err != nil && c.logger != nil {
			c.logger.Warn("Failed to cache member alias",
				slog.String("alias", alias),
				slog.Any("error", err))
		}
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
		if channelIDs, ok := val.([]string); ok {
			return channelIDs, nil
		}
		c.allMembers.Delete(allChannelIDsKey)
	}

	channelIDs, err := c.repository.GetAllChannelIDs(ctx)
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
	c.byChannelID.Clear()
	c.byName.Clear()
	c.allMembers.Clear()
	c.allMembersSnapshot.Store(nil)

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

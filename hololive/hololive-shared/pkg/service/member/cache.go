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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	memberChannelKeyPrefix = "member:channel:"
	memberNameKeyPrefix    = "member:name:"
	memberAliasKeyPrefix   = "member:alias:"
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

	snapshotMu             sync.RWMutex
	epochMu                sync.Mutex
	snapshotGeneration     atomic.Uint64
	allMembersSnapshot     atomic.Pointer[allMembersState]
	allMembersGroup        singleflight.Group
	snapshotTTL            time.Duration
	loadAllMembers         func(ctx context.Context) ([]*domain.Member, error)
	epoch                  memberEpochAuthority
	authorityEpoch         atomic.Uint64
	authorityHealthy       atomic.Bool
	epochReconcileInterval time.Duration

	cacheTTL time.Duration
	warmup   bool

	warmUpChunkSize     int
	warmUpMaxGoroutines int
}

type CacheConfig struct {
	ValkeyTTL              time.Duration
	EpochReconcileInterval time.Duration
	WarmUp                 bool // 시작 시 전체 멤버를 메모리에 로드
	WarmUpChunkSize        int
	WarmUpMaxGoroutines    int
}

type memoryMember struct {
	member     *domain.Member
	generation uint64
}

// 설정에 따라 생성 시점에 자동으로 캐시 워밍업을 수행할 수 있다.
func NewMemberCache(ctx context.Context, repository *Repository, cacheService cache.KeyValueCache, logger *slog.Logger, config CacheConfig) (*Cache, error) {
	ctx = memberCacheContext(ctx)
	config = normalizeMemberCacheConfig(config)

	mc := newMemberCache(repository, cacheService, logger, config)

	if err := mc.configureEpoch(ctx, cacheService); err != nil {
		return nil, fmt.Errorf("configure epoch: %w", err)
	}

	if config.WarmUp {
		mc.warmUpAtStartup(ctx)
	}

	return mc, nil
}

func memberCacheContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

func memberEpochRuntimeContext(ctx context.Context) context.Context {
	return context.WithoutCancel(memberCacheContext(ctx))
}

func normalizeMemberCacheConfig(config CacheConfig) CacheConfig {
	if config.ValkeyTTL == 0 {
		config.ValkeyTTL = constants.MemberCacheDefaults.ValkeyTTL
	}

	if config.WarmUpChunkSize == 0 {
		config.WarmUpChunkSize = constants.MemberCacheDefaults.WarmUpChunkSize
	}

	if config.WarmUpMaxGoroutines == 0 {
		config.WarmUpMaxGoroutines = constants.MemberCacheDefaults.WarmUpMaxGoroutines
	}

	if config.EpochReconcileInterval == 0 {
		config.EpochReconcileInterval = constants.MemberCacheDefaults.EpochReconcileInterval
	}

	return config
}

func newMemberCache(repository *Repository, cacheService cache.KeyValueCache, logger *slog.Logger, config CacheConfig) *Cache {
	return &Cache{
		repository:             repository,
		cache:                  cacheService,
		logger:                 logger,
		cacheTTL:               config.ValkeyTTL,
		warmup:                 config.WarmUp,
		snapshotTTL:            allMembersSnapshotTTL,
		epochReconcileInterval: config.EpochReconcileInterval,

		warmUpChunkSize:     config.WarmUpChunkSize,
		warmUpMaxGoroutines: config.WarmUpMaxGoroutines,
	}
}

func (c *Cache) configureEpoch(ctx context.Context, cacheService cache.KeyValueCache) error {
	if cacheService == nil {
		return nil
	}

	lowLevel, ok := cacheService.(cache.LowLevelCache)
	if !ok || lowLevel.GetClient() == nil {
		return errors.New("member cache requires low-level Valkey access for epoch coordination")
	}

	c.epoch = newValkeyMemberEpochAuthority(lowLevel.GetClient())
	if err := c.reconcileEpoch(ctx, epochReconcileStartup); err != nil && c.logger != nil {
		c.logger.Warn("member cache epoch unavailable at startup; cache bypass enabled", slog.Any("error", err))
	}

	panicguard.Go(c.logger, "member-cache-epoch-subscription", func() {
		c.runEpochReconciliation(memberEpochRuntimeContext(ctx))
	})

	return nil
}

func (c *Cache) warmUpAtStartup(ctx context.Context) {
	if err := c.WarmUpCache(ctx); err != nil && c.logger != nil {
		c.logger.Warn("Failed to warm up member cache", slog.Any("error", err))
	}
}

func (c *Cache) cacheEnabled() bool {
	return c != nil && c.cache != nil
}

func (c *Cache) GetByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	if c.cacheBypassRequired("channel") {
		out, err := c.repository.FindByChannelID(ctx, channelID)
		if err != nil {
			return nil, fmt.Errorf("find by channel ID: %w", err)
		}

		return out, nil
	}

	if member, ok := c.loadChannelFromMemory(channelID); ok {
		return member, nil
	}

	generation := c.currentSnapshotGeneration()
	if member := c.loadChannelFromDistributedCache(ctx, channelID, generation); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find by channel ID: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(ctx, dbMember, generation, "")
	}

	return dbMember, nil
}

func (c *Cache) GetByName(ctx context.Context, name string) (*domain.Member, error) {
	if c.cacheBypassRequired("name") {
		out, err := c.repository.FindByName(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("find by name: %w", err)
		}

		return out, nil
	}

	if member, ok := c.loadNameFromMemory(name); ok {
		return member, nil
	}

	generation := c.currentSnapshotGeneration()
	if member := c.loadNameFromDistributedCache(ctx, name, generation); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("find by name: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(ctx, dbMember, generation, "")
	}

	return dbMember, nil
}

func (c *Cache) loadChannelFromMemory(channelID string) (*domain.Member, bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	val, ok := c.byChannelID.Load(channelID)
	if !ok {
		return nil, false
	}

	if member, ok := val.(*memoryMember); ok {
		return member.member, true
	}

	c.byChannelID.Delete(channelID)

	return nil, false
}

func (c *Cache) loadNameFromMemory(name string) (*domain.Member, bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	val, ok := c.byName.Load(name)
	if !ok {
		return nil, false
	}

	if member, ok := val.(*memoryMember); ok {
		return member.member, true
	}

	c.byName.Delete(name)

	return nil, false
}

func (c *Cache) loadChannelFromDistributedCache(ctx context.Context, channelID string, generation uint64) *domain.Member {
	if !c.distributedCacheUsable() {
		return nil
	}

	cacheKey := c.epochDataKey(memberChannelKeyPrefix + channelID)

	var member domain.Member

	if err := c.cache.Get(ctx, cacheKey, &member); err != nil || member.Name == "" {
		return nil
	}

	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return nil
	}

	owned := c.snapshotOwnedChannelMemberLocked(channelID, &member, generation)
	if owned != nil {
		c.storePointMemberInMemoryLocked(owned, generation)
	}

	return owned
}

func (c *Cache) loadNameFromDistributedCache(ctx context.Context, name string, generation uint64) *domain.Member {
	if !c.distributedCacheUsable() {
		return nil
	}

	cacheKey := c.epochDataKey(memberNameKeyPrefix + name)

	var member domain.Member

	if err := c.cache.Get(ctx, cacheKey, &member); err != nil || member.Name == "" {
		return nil
	}

	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return nil
	}

	owned := c.snapshotOwnedNameMemberLocked(name, &member, generation)
	if owned != nil {
		c.storePointMemberInMemoryLocked(owned, generation)
	}

	return owned
}

// 별명 조회 성공 시 해당 멤버 정보를 캐시에 등록한다.

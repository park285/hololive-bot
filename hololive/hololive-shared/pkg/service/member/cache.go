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

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	allChannelIDsKey      = "channel_ids"
	allMembersSnapshotKey = "all_members"
)

// Cache는 PostgreSQL members를 프로세스 안 snapshot과 point index로 캐시한다. Valkey에는 멤버 데이터를 두지 않고,
// durable epoch(coord:member-cache:v2:epoch)와 그 통지 채널만 써서 다른 프로세스의 snapshot 무효화를 조정한다.
type Cache struct {
	repository *Repository
	logger     *slog.Logger

	byChannelID sync.Map // map[string]*memoryMember
	byName      sync.Map // map[string]*memoryMember
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
	epochRuntimeCancel     context.CancelFunc
	epochRuntimeDone       <-chan struct{}
}

type CacheConfig struct {
	EpochReconcileInterval time.Duration
	WarmUp                 bool // 시작 시 전체 멤버를 메모리에 로드
}

type memoryMember struct {
	member     *domain.Member
	generation uint64
}

// epochStore가 nil이면 epoch 조정 없는 프로세스 로컬 캐시가 된다. 설정에 따라 생성 시점에 snapshot을 미리 적재한다.
func NewMemberCache(ctx context.Context, repository *Repository, epochStore cache.LowLevelCache, logger *slog.Logger, config CacheConfig) (*Cache, error) {
	config = normalizeMemberCacheConfig(config)

	mc := newMemberCache(repository, logger, config)

	if err := mc.configureEpoch(ctx, epochStore); err != nil {
		return nil, fmt.Errorf("configure epoch: %w", err)
	}

	if config.WarmUp {
		mc.warmUpAtStartup(ctx)
	}

	return mc, nil
}

func memberEpochRuntimeContext(ctx context.Context) context.Context {
	return context.WithoutCancel(ctx)
}

func normalizeMemberCacheConfig(config CacheConfig) CacheConfig {
	if config.EpochReconcileInterval == 0 {
		config.EpochReconcileInterval = constants.MemberCacheDefaults.EpochReconcileInterval
	}

	return config
}

func newMemberCache(repository *Repository, logger *slog.Logger, config CacheConfig) *Cache {
	return &Cache{
		repository:             repository,
		logger:                 logger,
		snapshotTTL:            allMembersSnapshotTTL,
		epochReconcileInterval: config.EpochReconcileInterval,
	}
}

func (c *Cache) configureEpoch(ctx context.Context, epochStore cache.LowLevelCache) error {
	if epochStore == nil {
		return nil
	}

	client := epochStore.GetClient()
	if client == nil {
		return errors.New("member cache requires low-level Valkey access for epoch coordination")
	}

	c.epoch = newValkeyMemberEpochAuthority(client)
	if err := c.reconcileEpoch(ctx, epochReconcileStartup); err != nil && c.logger != nil {
		c.logger.Warn("member cache epoch unavailable at startup; cache bypass enabled", slog.Any("error", err))
	}

	c.startEpochReconciliation(ctx)

	return nil
}

func (c *Cache) startEpochReconciliation(ctx context.Context) {
	runtimeCtx, cancel := context.WithCancel(memberEpochRuntimeContext(ctx))
	done := make(chan struct{})

	c.epochRuntimeCancel = cancel
	c.epochRuntimeDone = done

	go panicguard.Run(c.logger, panicguard.BackgroundTask, "member-cache-epoch-subscription", func() {
		defer close(done)

		c.runEpochReconciliation(runtimeCtx)
	})
}

// Close는 epoch 구독과 재조회 작업을 취소하고 둘 다 끝날 때까지 기다린다.
// Valkey 자원을 닫기 전에 호출하며, nil 캐시와 반복 호출도 안전하다.
func (c *Cache) Close() {
	if c == nil || c.epochRuntimeCancel == nil {
		return
	}

	c.epochRuntimeCancel()
	<-c.epochRuntimeDone
}

func (c *Cache) warmUpAtStartup(ctx context.Context) {
	if err := c.WarmUpCache(ctx); err != nil && c.logger != nil {
		c.logger.Warn("Failed to warm up member cache", slog.Any("error", err))
	}
}

// epochCoordinated는 epoch authority로 다른 프로세스와 무효화를 조정하는 구성인지 알려 준다.
func (c *Cache) epochCoordinated() bool {
	return c != nil && c.epoch != nil
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

	dbMember, err := c.repository.FindByChannelID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find by channel ID: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(dbMember, generation, true)
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

	dbMember, err := c.repository.FindByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("find by name: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(dbMember, generation, false)
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

// FindByAlias는 게시된 snapshot에서 별칭을 먼저 찾는다. Snapshot이 없거나 snapshot에 없는 별칭은 예전 L2 miss와 같이
// PostgreSQL을 조회한다(epoch 무효화 없이 바뀐 행도 곧바로 보이도록 음성 결과는 캐시하지 않는다).
func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if c.cacheBypassRequired("alias") {
		out, err := c.repository.FindByAlias(ctx, alias)
		if err != nil {
			return nil, fmt.Errorf("find by alias: %w", err)
		}

		return out, nil
	}

	member, generation := c.findAliasInSnapshot(alias)
	if member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByAlias(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("find by alias: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(dbMember, generation, false)
	}

	return dbMember, nil
}

func (c *Cache) findAliasInSnapshot(alias string) (*domain.Member, uint64) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	generation := c.snapshotGeneration.Load()

	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) || snap.generation != generation {
		return nil, generation
	}

	return snap.aliasOwner(alias), generation
}

func (c *Cache) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	if c.cacheBypassRequired("channel_ids") {
		out, err := c.repository.GetAllChannelIDs(ctx)
		if err != nil {
			return out, fmt.Errorf("get all channel IDs: %w", err)
		}

		return out, nil
	}

	c.snapshotMu.RLock()

	if val, ok := c.allMembers.Load(allChannelIDsKey); ok {
		if channelIDs, ok := val.([]string); ok {
			c.snapshotMu.RUnlock()

			return channelIDs, nil
		}

		c.allMembers.Delete(allChannelIDsKey)
	}

	c.snapshotMu.RUnlock()

	generation := c.currentSnapshotGeneration()

	channelIDs, err := c.repository.GetAllChannelIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all channel IDs: %w", err)
	}

	c.snapshotMu.RLock()

	if c.snapshotGeneration.Load() == generation {
		c.allMembers.Store(allChannelIDsKey, channelIDs)
	}

	c.snapshotMu.RUnlock()

	return channelIDs, nil
}

func (c *Cache) currentSnapshotGeneration() uint64 {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	return c.snapshotGeneration.Load()
}

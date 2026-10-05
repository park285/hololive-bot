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

const allMembersSnapshotKey = "all_members"

// Cache는 PostgreSQL members를 프로세스 안 snapshot과 point overlay로 캐시한다. Valkey에는 멤버 데이터를 두지 않고,
// durable epoch(coord:member-cache:v2:epoch)와 그 통지 채널만 써서 다른 프로세스의 snapshot 무효화를 조정한다.
//
// SnapshotMu가 snapshotGeneration·allMembersSnapshot·pointOverlay를 함께 보호한다. 게시된 snapshot과 색인은 불변이고,
// point overlay는 snapshot에 없는 PostgreSQL 단건 결과만 조회 시점 generation 태그로 담는다. Generation이 바뀌면
// overlay는 통째로 무효다.
type Cache struct {
	repository *Repository
	logger     *slog.Logger

	snapshotMu             sync.RWMutex
	epochMu                sync.Mutex
	snapshotGeneration     atomic.Uint64
	allMembersSnapshot     atomic.Pointer[allMembersState]
	pointOverlay           *memberPointOverlay
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

// resetMemoryLocked는 snapshot과 point overlay를 버리고 generation을 올린다. 진행 중인 적재·point 조회는 이전
// generation을 들고 있으므로 결과를 게시하지 못한다. 호출자가 snapshotMu 쓰기 잠금을 쥔다.
func (c *Cache) resetMemoryLocked() {
	c.snapshotGeneration.Add(1)

	c.pointOverlay = nil
	c.allMembersSnapshot.Store(nil)
}

// epochStore가 nil이면 epoch 조정 없는 프로세스 로컬 캐시가 된다. 설정에 따라 생성 시점에 snapshot을 미리 적재한다.
// PostgreSQL pool 없는 repository는 첫 조회에서 panic하므로 구성 단계에서 거절한다.
func NewMemberCache(ctx context.Context, repository *Repository, epochStore cache.LowLevelCache, logger *slog.Logger, config CacheConfig) (*Cache, error) {
	if repository != nil && repository.pool == nil {
		return nil, errors.New("member repository has no PostgreSQL pool")
	}

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

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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const allMembersSnapshotKey = "all_members"

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

const defaultEpochReconcileInterval = 15 * time.Second

type CacheConfig struct {
	EpochReconcileInterval time.Duration
	WarmUp                 bool
}

func (c *Cache) resetMemoryLocked() {
	c.snapshotGeneration.Add(1)

	c.pointOverlay = nil
	c.allMembersSnapshot.Store(nil)
}

func NewMemberCache(ctx context.Context, repository *Repository, epochStore cache.LowLevelCache, logger *slog.Logger, config CacheConfig) (*Cache, error) {
	if repository != nil && repository.pool == nil {
		return nil, errors.New("member repository has no PostgreSQL pool")
	}

	if epochStore == nil {
		return nil, errors.New("member cache requires an epoch store")
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
		config.EpochReconcileInterval = defaultEpochReconcileInterval
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

	go panicguard.Run(c.logger, panicscope.BackgroundTask, "member-cache-epoch-subscription", func() {
		defer close(done)

		c.runEpochReconciliation(runtimeCtx)
	})
}

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

func (c *Cache) epochCoordinated() bool {
	return c != nil && c.epoch != nil
}

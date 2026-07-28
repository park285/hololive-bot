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
	"strconv"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	// plane마다 별도 Cache 인스턴스라 admin의 InvalidateAll이 bot plane 스냅샷에 닿지 않는다. 이 TTL이 그 cross-plane staleness의 유일한 상한이다.
	allMembersSnapshotTTL         = 5 * time.Minute
	allMembersSnapshotLoadTimeout = 10 * time.Second
	allMembersSnapshotRetryDelay  = time.Minute
)

type allMembersState struct {
	members       []*domain.Member
	loadedAt      time.Time
	retryAfter    time.Time
	loadErr       error
	generation    uint64
	hasSuccessful bool
}

var errAllMembersGenerationChanged = errors.New("member snapshot generation changed")

func (c *Cache) AllMembers(ctx context.Context) ([]*domain.Member, error) {
	if c == nil {
		return nil, fmt.Errorf("member cache is nil")
	}

	for {
		snap, generation := c.allMembersView()
		if members, err, ready := c.snapshotResultAt(snap, time.Now()); ready {
			return cloneAllMembersResult(members, err)
		}

		members, err, retry := c.loadAllMembersResult(ctx, snap, generation)
		if retry {
			continue
		}
		return cloneAllMembersResult(members, err)
	}
}

func cloneAllMembersResult(members []*domain.Member, err error) ([]*domain.Member, error) {
	if err != nil {
		return nil, err
	}
	return cloneMemberSlice(members), nil
}

func (c *Cache) loadAllMembersResult(
	ctx context.Context,
	snap *allMembersState,
	generation uint64,
) ([]*domain.Member, error, bool) {
	members, err := c.loadAllMembersSnapshot(ctx, snap, generation)
	if err == nil {
		return members, nil, false
	}
	if errors.Is(err, errAllMembersGenerationChanged) {
		return nil, nil, true
	}
	return c.failedAllMembersResult(snap, generation, err)
}

func (c *Cache) failedAllMembersResult(
	snap *allMembersState,
	generation uint64,
	err error,
) ([]*domain.Member, error, bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()
	if c.snapshotGeneration.Load() != generation {
		return nil, nil, true
	}
	if snapshotSuccessful(snap) {
		return snap.members, nil, false
	}
	return nil, err, false
}

func (c *Cache) snapshotResultAt(snap *allMembersState, now time.Time) ([]*domain.Member, error, bool) {
	if c.snapshotFreshAt(snap, now) {
		return snap.members, nil, true
	}
	if !c.snapshotReloadDeferred(snap, now) {
		return nil, nil, false
	}
	if snapshotSuccessful(snap) {
		return snap.members, nil, true
	}
	return nil, snap.loadErr, true
}

func (c *Cache) snapshotFreshAt(snap *allMembersState, now time.Time) bool {
	if !snapshotSuccessful(snap) {
		return false
	}
	if c.snapshotTTL <= 0 {
		return true
	}
	return now.Sub(snap.loadedAt) < c.snapshotTTL
}

func (*Cache) snapshotReloadDeferred(snap *allMembersState, now time.Time) bool {
	return snap != nil && !snap.retryAfter.IsZero() && now.Before(snap.retryAfter)
}

func (c *Cache) loadAllMembersSnapshot(ctx context.Context, _ *allMembersState, generation uint64) ([]*domain.Member, error) {
	loader, err := c.allMembersLoader()
	if err != nil {
		return nil, err
	}
	groupKey := allMembersSnapshotKey + ":" + strconv.FormatUint(generation, 10)
	result, err, _ := c.allMembersGroup.Do(groupKey, func() (any, error) {
		return c.reloadAllMembersSnapshot(ctx, loader, generation)
	})
	if err != nil {
		return nil, err
	}

	members, ok := result.([]*domain.Member)
	if !ok {
		return nil, fmt.Errorf("unexpected all members result type %T", result)
	}
	return cloneMemberSlice(members), nil
}

func (c *Cache) allMembersLoader() (func(context.Context) ([]*domain.Member, error), error) {
	if c.loadAllMembers != nil {
		return c.loadAllMembers, nil
	}
	if c.repository == nil {
		return nil, fmt.Errorf("member repository is nil")
	}
	return c.repository.GetAllMembers, nil
}

func (c *Cache) reloadAllMembersSnapshot(
	ctx context.Context,
	loader func(context.Context) ([]*domain.Member, error),
	generation uint64,
) ([]*domain.Member, error) {
	current, currentGeneration := c.allMembersView()
	if currentGeneration != generation {
		return nil, errAllMembersGenerationChanged
	}
	if members, err, ready := c.snapshotResultAt(current, time.Now()); ready {
		return members, err
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), allMembersSnapshotLoadTimeout)
	defer cancel()
	members, err := loader(loadCtx)
	if err != nil {
		loadErr := fmt.Errorf("load all members from repository: %w", err)
		if !c.deferAllMembersSnapshotReload(current, generation, loadErr) {
			return nil, errAllMembersGenerationChanged
		}
		return nil, loadErr
	}
	if !c.storeAllMembersSnapshot(current, generation, members) {
		return nil, errAllMembersGenerationChanged
	}
	c.logAllMembersSnapshotRecovery(current, len(members))
	return members, nil
}

func (c *Cache) deferAllMembersSnapshotReload(snap *allMembersState, generation uint64, err error) bool {
	retryAfter := time.Now().Add(allMembersSnapshotRetryDelay)
	deferred := &allMembersState{
		retryAfter: retryAfter,
		loadErr:    err,
		generation: generation,
	}
	if snapshotSuccessful(snap) {
		deferred.members = snap.members
		deferred.loadedAt = snap.loadedAt
		deferred.hasSuccessful = true
	}

	c.snapshotMu.Lock()
	retryScheduled := c.snapshotGeneration.Load() == generation && c.allMembersSnapshot.Load() == snap
	if retryScheduled {
		c.allMembersSnapshot.Store(deferred)
	}
	c.snapshotMu.Unlock()
	if c.logger != nil {
		c.logger.Warn("member_snapshot_reload_failed",
			slog.Bool("stale_available", snapshotSuccessful(deferred)),
			slog.Bool("retry_scheduled", retryScheduled),
			slog.Time("retry_after", retryAfter),
			slog.Any("error", err),
		)
	}
	return retryScheduled
}

func (c *Cache) logAllMembersSnapshotRecovery(snap *allMembersState, memberCount int) {
	if snap == nil || snap.retryAfter.IsZero() || c.logger == nil {
		return
	}
	c.logger.Info("member_snapshot_reload_recovered", slog.Int("member_count", memberCount))
}

func (c *Cache) storeAllMembersSnapshot(previous *allMembersState, generation uint64, members []*domain.Member) bool {
	snapshot, channelIDs := prepareAllMembersSnapshot(members)
	c.cacheIOMu.Lock()
	defer c.cacheIOMu.Unlock()
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	if c.snapshotGeneration.Load() != generation || c.allMembersSnapshot.Load() != previous {
		return false
	}

	nextGeneration := generation + 1
	c.replaceMemberSnapshotIndexes(snapshot, generation, nextGeneration)
	c.allMembers.Store(allChannelIDsKey, channelIDs)
	c.snapshotGeneration.Store(nextGeneration)
	c.allMembersSnapshot.Store(&allMembersState{
		members:       snapshot,
		loadedAt:      time.Now(),
		generation:    nextGeneration,
		hasSuccessful: true,
	})
	return true
}

func prepareAllMembersSnapshot(members []*domain.Member) (snapshot []*domain.Member, channelIDs []string) {
	snapshot = make([]*domain.Member, 0, len(members))
	channelIDs = make([]string, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		snapshot = append(snapshot, member)
		if member.ChannelID != "" {
			channelIDs = append(channelIDs, member.ChannelID)
		}
	}
	return snapshot, channelIDs
}

func (c *Cache) replaceMemberSnapshotIndexes(members []*domain.Member, generation, nextGeneration uint64) {
	deleteMemberGeneration(&c.byChannelID, generation)
	deleteMemberGeneration(&c.byName, generation)
	for _, member := range members {
		entry := &memoryMember{member: member, generation: nextGeneration}
		if member.ChannelID != "" {
			c.byChannelID.Store(member.ChannelID, entry)
		}
		c.byName.Store(member.Name, entry)
	}
}

func deleteMemberGeneration(index *sync.Map, generation uint64) {
	index.Range(func(key, value any) bool {
		entry, ok := value.(*memoryMember)
		if ok && entry.generation == generation {
			index.CompareAndDelete(key, value)
		}
		return true
	})
}

func (c *Cache) allMembersView() (snapshot *allMembersState, generation uint64) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()
	return c.allMembersSnapshot.Load(), c.snapshotGeneration.Load()
}

func snapshotSuccessful(snap *allMembersState) bool {
	return snap != nil && (snap.hasSuccessful || !snap.loadedAt.IsZero())
}

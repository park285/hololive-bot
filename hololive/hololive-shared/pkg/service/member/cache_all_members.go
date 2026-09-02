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
	// Durable epoch가 freshness를 소유하고, 이 TTL은 같은 epoch 안의 정기 DB refresh 상한을 둔다.
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
		return nil, errors.New("member cache is nil")
	}

	for {
		if c.cacheBypassRequired("all_members") {
			return c.loadAllMembersBypass(ctx)
		}

		snap, generation := c.allMembersView()
		if members, ready, snapErr := c.snapshotResultAt(snap, time.Now()); ready {
			return cloneAllMembersResult(members, snapErr)
		}

		members, retry, err := c.loadAllMembersResult(ctx, snap, generation)
		if retry {
			continue
		}

		return cloneAllMembersResult(members, err)
	}
}

func (c *Cache) loadAllMembersBypass(ctx context.Context) ([]*domain.Member, error) {
	loader, err := c.allMembersLoader()
	if err != nil {
		return nil, err
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), allMembersSnapshotLoadTimeout)

	defer cancel()

	members, err := loader(loadCtx)
	if err != nil {
		return nil, fmt.Errorf("load all members from repository while cache bypassed: %w", err)
	}

	return cloneMemberSlice(members), nil
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
) ([]*domain.Member, bool, error) {
	members, err := c.loadAllMembersSnapshot(ctx, snap, generation)
	if err == nil {
		return members, false, nil
	}

	if errors.Is(err, errAllMembersGenerationChanged) {
		return nil, true, nil
	}

	stale, retry, usable := c.staleAllMembersResult(snap, generation)
	if retry {
		return nil, true, nil
	}

	if usable {
		return stale, false, nil
	}

	return nil, false, err
}

func (c *Cache) staleAllMembersResult(
	snap *allMembersState,
	generation uint64,
) (members []*domain.Member, retry, usable bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return nil, true, false
	}

	if snapshotSuccessful(snap) {
		return snap.members, false, true
	}

	return nil, false, false
}

func (c *Cache) snapshotResultAt(snap *allMembersState, now time.Time) ([]*domain.Member, bool, error) {
	if c.snapshotFreshAt(snap, now) {
		return snap.members, true, nil
	}

	if !c.snapshotReloadDeferred(snap, now) {
		return nil, false, nil
	}

	if snapshotSuccessful(snap) {
		return snap.members, true, nil
	}

	return nil, true, snap.loadErr
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
		return nil, errors.New("member repository is nil")
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

	if members, ready, err := c.snapshotResultAt(current, time.Now()); ready {
		out, snapshotErr := completedAllMembersSnapshot(members, err)

		return out, errors.Join(snapshotErr)
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), allMembersSnapshotLoadTimeout)
	defer cancel()

	members, err := loader(loadCtx)
	if err != nil {
		if failureErr := c.handleAllMembersLoadFailure(loadCtx, current, generation, err); failureErr != nil {
			return nil, failureErr
		}

		return nil, nil
	}

	if err := c.confirmEpochAfterLoad(loadCtx, generation); err != nil {
		return nil, fmt.Errorf("confirm epoch after load: %w", err)
	}

	if !c.storeAllMembersSnapshot(current, generation, members) {
		return nil, errAllMembersGenerationChanged
	}

	c.logAllMembersSnapshotRecovery(current, len(members))

	return members, nil
}

func completedAllMembersSnapshot(members []*domain.Member, err error) ([]*domain.Member, error) {
	if err != nil {
		return nil, err
	}

	return members, nil
}

func (c *Cache) handleAllMembersLoadFailure(
	ctx context.Context,
	current *allMembersState,
	generation uint64,
	loadFailure error,
) error {
	loadErr := fmt.Errorf("load all members from repository: %w", loadFailure)

	if err := c.confirmEpochAfterLoad(ctx, generation); err != nil {
		return fmt.Errorf("confirm epoch after load: %w", err)
	}

	if !c.deferAllMembersSnapshotReload(current, generation, loadErr) {
		return errAllMembersGenerationChanged
	}

	return loadErr
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

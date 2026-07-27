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
	members    []*domain.Member
	loadedAt   time.Time
	retryAfter time.Time
}

func (c *Cache) AllMembers(ctx context.Context) ([]*domain.Member, error) {
	if c == nil {
		return nil, fmt.Errorf("member cache is nil")
	}

	snap := c.allMembersSnapshot.Load()
	now := time.Now()
	if c.snapshotFreshAt(snap, now) || c.snapshotReloadDeferred(snap, now) {
		return cloneMemberSlice(snap.members), nil
	}

	members, err := c.loadAllMembersSnapshot(ctx, snap)
	if err != nil {
		if snap != nil {
			return cloneMemberSlice(snap.members), nil
		}
		return nil, err
	}
	return members, nil
}

func (c *Cache) snapshotFreshAt(snap *allMembersState, now time.Time) bool {
	if snap == nil {
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

func (c *Cache) loadAllMembersSnapshot(ctx context.Context, snap *allMembersState) ([]*domain.Member, error) {
	loader := c.loadAllMembers
	if loader == nil {
		if c.repository == nil {
			return nil, fmt.Errorf("member repository is nil")
		}
		loader = c.repository.GetAllMembers
	}

	result, err, _ := c.allMembersGroup.Do(allMembersSnapshotKey, func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), allMembersSnapshotLoadTimeout)
		defer cancel()

		members, err := loader(loadCtx)
		if err != nil {
			c.deferAllMembersSnapshotReload(snap, err)
			return nil, fmt.Errorf("load all members from repository: %w", err)
		}

		c.storeAllMembersSnapshot(members)
		c.logAllMembersSnapshotRecovery(snap, len(members))
		return members, nil
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

func (c *Cache) deferAllMembersSnapshotReload(snap *allMembersState, err error) {
	if snap == nil {
		if c.logger != nil {
			c.logger.Warn("member_snapshot_reload_failed",
				slog.Bool("stale_available", false),
				slog.Any("error", err),
			)
		}
		return
	}

	retryAfter := time.Now().Add(allMembersSnapshotRetryDelay)
	deferred := &allMembersState{
		members:    snap.members,
		loadedAt:   snap.loadedAt,
		retryAfter: retryAfter,
	}
	retryScheduled := c.allMembersSnapshot.CompareAndSwap(snap, deferred)
	if c.logger != nil {
		c.logger.Warn("member_snapshot_reload_failed",
			slog.Bool("stale_available", true),
			slog.Bool("retry_scheduled", retryScheduled),
			slog.Time("retry_after", retryAfter),
			slog.Any("error", err),
		)
	}
}

func (c *Cache) logAllMembersSnapshotRecovery(snap *allMembersState, memberCount int) {
	if snap == nil || snap.retryAfter.IsZero() || c.logger == nil {
		return
	}
	c.logger.Info("member_snapshot_reload_recovered", slog.Int("member_count", memberCount))
}

func (c *Cache) storeAllMembersSnapshot(members []*domain.Member) {
	snapshot := make([]*domain.Member, 0, len(members))
	channelIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		snapshot = append(snapshot, member)
		if member.ChannelID != "" {
			c.byChannelID.Store(member.ChannelID, member)
			channelIDs = append(channelIDs, member.ChannelID)
		}
		c.byName.Store(member.Name, member)
	}

	c.allMembers.Store(allChannelIDsKey, channelIDs)
	c.allMembersSnapshot.Store(&allMembersState{members: snapshot, loadedAt: time.Now()})
}

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

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if c.cacheBypassRequired("alias") {
		out, err := c.repository.FindByAlias(ctx, alias)
		if err != nil {
			return nil, fmt.Errorf("find by alias: %w", err)
		}

		return out, nil
	}

	generation := c.currentSnapshotGeneration()
	if member := c.getAliasFromCache(ctx, alias, generation); member != nil {
		return member, nil
	}

	dbMember, err := c.repository.FindByAlias(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("find by alias: %w", err)
	}

	if dbMember != nil {
		c.cacheMember(ctx, dbMember, generation, alias)
	}

	return dbMember, nil
}

func (c *Cache) getAliasFromCache(ctx context.Context, alias string, generation uint64) *domain.Member {
	if !c.distributedCacheUsable() {
		return nil
	}

	c.snapshotMu.RLock()

	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return nil
	}

	cacheKey := c.epochDataKey(memberAliasKeyPrefix + alias)

	var member domain.Member

	if err := c.cache.Get(ctx, cacheKey, &member); err != nil || member.Name == "" {
		return nil
	}

	owned := c.snapshotOwnedAliasMemberLocked(alias, &member, generation)
	if owned != nil {
		c.storePointMemberInMemoryLocked(owned, generation)
	}

	return owned
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

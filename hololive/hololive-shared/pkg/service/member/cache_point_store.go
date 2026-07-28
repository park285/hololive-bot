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
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Cache) cacheMember(ctx context.Context, member *domain.Member, generation uint64, alias string) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()
	if c.snapshotGeneration.Load() != generation {
		return
	}

	c.storePointMemberInMemoryLocked(member, generation)
	if !c.cacheEnabled() {
		return
	}
	c.cacheMemberByChannelID(ctx, member)
	c.cacheMemberByName(ctx, member)
	c.cacheMemberByAlias(ctx, member, alias)
}

func (c *Cache) cacheMemberByChannelID(ctx context.Context, member *domain.Member) {
	if member.ChannelID == "" {
		return
	}
	channelKey := memberChannelKeyPrefix + member.ChannelID
	if err := c.cache.Set(ctx, channelKey, member, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to cache member by channel ID",
			slog.String("channel_id", member.ChannelID),
			slog.Any("error", err),
		)
	}
}

func (c *Cache) cacheMemberByName(ctx context.Context, member *domain.Member) {
	nameKey := memberNameKeyPrefix + member.Name
	if err := c.cache.Set(ctx, nameKey, member, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to cache member by name",
			slog.String("member", member.Name),
			slog.Any("error", err),
		)
	}
}

func (c *Cache) cacheMemberByAlias(ctx context.Context, member *domain.Member, alias string) {
	if alias == "" {
		return
	}
	aliasKey := memberAliasKeyPrefix + alias
	if err := c.cache.Set(ctx, aliasKey, member, c.cacheTTL); err != nil && c.logger != nil {
		c.logger.Warn("Failed to cache member alias",
			slog.String("alias", alias),
			slog.Any("error", err))
	}
}

func (c *Cache) storePointMemberInMemoryLocked(member *domain.Member, generation uint64) {
	entry := &memoryMember{member: member, generation: generation}
	if member.ChannelID != "" {
		c.byChannelID.Store(member.ChannelID, entry)
	}
	c.byName.Store(member.Name, entry)
}

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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

// 병렬 처리를 통해 대량의 데이터도 빠르게 처리한다.
func (c *Cache) WarmUpCache(ctx context.Context) error {
	if c == nil {
		return errors.New("member cache is nil")
	}

	snap, generation := c.allMembersView()

	members, err := c.loadAllMembersSnapshot(ctx, snap, generation)
	if err != nil {
		return fmt.Errorf("failed to load all members: %w", err)
	}

	warmupGeneration, ok := c.snapshotGenerationForMembers(members)
	if !ok {
		return fmt.Errorf("failed to load all members: %w", errAllMembersGenerationChanged)
	}

	chunkSize := c.warmUpChunkSize
	chunks := chunkMembers(members, chunkSize)

	maxWorkers := max(1, c.warmUpMaxGoroutines)
	semaphore := make(chan struct{}, maxWorkers)

	var wg sync.WaitGroup

	for _, chunk := range chunks {
		wg.Go(func() {
			panicguard.Run(c.logger, "member-cache-warmup", func() {
				semaphore <- struct{}{}

				defer func() { <-semaphore }()

				c.cacheChunk(ctx, chunk, warmupGeneration)
			})
		})
	}

	wg.Wait()

	if c.logger != nil {
		c.logger.Info("Member cache warmed up",
			slog.Int("total_members", len(members)),
			slog.Int("chunks", len(chunks)),
		)
	}

	return nil
}

func (c *Cache) cacheChunk(ctx context.Context, members []*domain.Member, generation uint64) {
	if len(members) == 0 {
		return
	}

	if !c.distributedCacheUsable() {
		return
	}

	pairs := make(map[string]any, len(members)*2)

	for _, member := range members {
		if member.ChannelID != "" {
			channelKey := c.epochDataKey(memberChannelKeyPrefix + member.ChannelID)

			pairs[channelKey] = member
		}

		nameKey := c.epochDataKey(memberNameKeyPrefix + member.Name)

		pairs[nameKey] = member
	}

	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return
	}

	if err := c.cache.MSet(ctx, pairs, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to batch cache members",
			slog.Int("count", len(members)),
			slog.Any("error", err))
	}
}

func (c *Cache) snapshotGenerationForMembers(members []*domain.Member) (uint64, bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) || len(snap.members) != len(members) {
		return 0, false
	}

	for i := range members {
		if snap.members[i] != members[i] {
			return 0, false
		}
	}

	return c.snapshotGeneration.Load(), true
}

func chunkMembers(members []*domain.Member, chunkSize int) [][]*domain.Member {
	var chunks [][]*domain.Member

	for i := 0; i < len(members); i += chunkSize {
		end := min(i+chunkSize, len(members))

		chunks = append(chunks, members[i:end])
	}

	return chunks
}

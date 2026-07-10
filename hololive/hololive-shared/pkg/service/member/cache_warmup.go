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
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

// 병렬 처리를 통해 대량의 데이터도 빠르게 처리한다.
func (c *Cache) WarmUpCache(ctx context.Context) error {
	members, err := c.repository.GetAllMembers(ctx)
	if err != nil {
		return fmt.Errorf("failed to load all members: %w", err)
	}

	chunkSize := c.warmUpChunkSize
	chunks := chunkMembers(members, chunkSize)

	maxWorkers := max(1, c.warmUpMaxGoroutines)
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, chunk := range chunks {
		wg.Add(1)
		panicguard.Go(c.logger, "member-cache-warmup", func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			c.cacheChunk(ctx, chunk)
		})
	}
	wg.Wait()

	for _, member := range members {
		if member.ChannelID != "" {
			c.byChannelID.Store(member.ChannelID, member)
		}
		c.byName.Store(member.Name, member)
	}

	c.logger.Info("Member cache warmed up",
		slog.Int("total_members", len(members)),
		slog.Int("chunks", len(chunks)),
	)

	return nil
}

func (c *Cache) cacheChunk(ctx context.Context, members []*domain.Member) {
	if len(members) == 0 {
		return
	}
	if !c.cacheEnabled() {
		return
	}

	pairs := make(map[string]any, len(members)*2)

	for _, member := range members {
		if member.ChannelID != "" {
			channelKey := memberChannelKeyPrefix + member.ChannelID
			pairs[channelKey] = member
		}

		nameKey := memberNameKeyPrefix + member.Name
		pairs[nameKey] = member
	}

	if err := c.cache.MSet(ctx, pairs, c.cacheTTL); err != nil {
		c.logger.Warn("Failed to batch cache members",
			slog.Int("count", len(members)),
			slog.Any("error", err))
	}
}

func chunkMembers(members []*domain.Member, chunkSize int) [][]*domain.Member {
	var chunks [][]*domain.Member
	for i := 0; i < len(members); i += chunkSize {
		end := min(i+chunkSize, len(members))
		chunks = append(chunks, members[i:end])
	}
	return chunks
}

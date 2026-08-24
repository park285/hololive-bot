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
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

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

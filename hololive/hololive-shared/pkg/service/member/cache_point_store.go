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
	"github.com/kapu/hololive-shared/pkg/domain"
)

// memberPointOverlay는 게시된 snapshot이 답하지 못한 PostgreSQL 단건 결과를 한 generation 동안만 담는다. SnapshotMu
// 쓰기 잠금 아래에서만 바뀌고, generation이 바뀌면 통째로 무효가 된다(snapshot 게시·무효화·epoch 변경 모두 generation을
// 올린다).
type memberPointOverlay struct {
	byChannelID   map[string]*domain.Member
	byName        map[string]*domain.Member
	channelIDs    []string
	generation    uint64
	hasChannelIDs bool
}

// currentOverlayLocked는 현재 generation의 overlay를 돌려준다. 호출자가 snapshotMu를 쥔다.
func (c *Cache) currentOverlayLocked(generation uint64) *memberPointOverlay {
	if c.pointOverlay == nil || c.pointOverlay.generation != generation {
		return nil
	}

	return c.pointOverlay
}

// overlayForWriteLocked는 generation이 현재 값과 같을 때만 쓸 overlay를 돌려준다. 늦게 끝난 이전 generation 조회는
// nil을 받아 아무것도 게시하지 못한다. 호출자가 snapshotMu 쓰기 잠금을 쥔다.
func (c *Cache) overlayForWriteLocked(generation uint64) *memberPointOverlay {
	if c.snapshotGeneration.Load() != generation {
		return nil
	}

	if overlay := c.currentOverlayLocked(generation); overlay != nil {
		return overlay
	}

	c.pointOverlay = &memberPointOverlay{
		byChannelID: make(map[string]*domain.Member),
		byName:      make(map[string]*domain.Member),
		generation:  generation,
	}

	return c.pointOverlay
}

// cacheMember는 PostgreSQL point 조회 결과를 조회 시점 generation의 overlay에만 넣는다. 채널 키는 같은 generation의
// snapshot 대표를 따르고, snapshot이 없으면 SQL 채널 대표 조회 결과만 채널 키를 채울 수 있다.
func (c *Cache) cacheMember(member *domain.Member, generation uint64, channelLookup bool) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	overlay := c.overlayForWriteLocked(generation)
	if overlay == nil {
		return
	}

	overlay.byName[member.Name] = member

	channelMember := c.channelMemberForPointLocked(member, generation, channelLookup)
	if channelMember != nil && channelMember.ChannelID != "" {
		overlay.byChannelID[channelMember.ChannelID] = channelMember
	}
}

// cacheChannelIDs는 snapshot이 없을 때 PostgreSQL 채널 ID 목록을 조회 시점 generation의 overlay에 넣는다.
func (c *Cache) cacheChannelIDs(channelIDs []string, generation uint64) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	overlay := c.overlayForWriteLocked(generation)
	if overlay == nil {
		return
	}

	overlay.channelIDs = channelIDs
	overlay.hasChannelIDs = true
}

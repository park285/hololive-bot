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

// cacheMember는 PostgreSQL point 조회 결과를 조회 시점 generation의 프로세스 메모리 index에만 넣는다.
func (c *Cache) cacheMember(member *domain.Member, generation uint64, channelLookup bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	if c.snapshotGeneration.Load() != generation {
		return
	}

	c.byName.Store(member.Name, &memoryMember{member: member, generation: generation})

	channelMember := c.channelMemberForPointLocked(member, generation, channelLookup)
	if channelMember != nil && channelMember.ChannelID != "" {
		c.byChannelID.Store(channelMember.ChannelID, &memoryMember{member: channelMember, generation: generation})
	}
}

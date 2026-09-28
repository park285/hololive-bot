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

package scraping

import (
	"strings"
	"sync"
	"time"
)

// cacheState는 채널별 boolean 상태를 process 메모리에만 TTL로 보관한다.
// 과거 Valkey stateStore 계층(youtube:producer:community-missing:*)은 운영에서 주입된 적이 없어 지웠다(Valkey 책임 축소 A12).
// 따라서 재시작하거나 runtime이 다르면 상태를 공유하지 않으며, 그 경우 한 번 더 조회하는 비용만 든다.
type cacheState struct {
	mu    sync.RWMutex
	until map[string]time.Time
	ttl   time.Duration
}

func newCacheState(ttl time.Duration) *cacheState {
	return &cacheState{
		until: make(map[string]time.Time),
		ttl:   ttl,
	}
}

func (cs *cacheState) isSet(key string) bool {
	now := time.Now()

	cs.mu.RLock()

	until, ok := cs.until[key]

	cs.mu.RUnlock()

	if ok && now.Before(until) {
		return true
	}

	if ok {
		cs.clearExpired(key, now)
	}

	return false
}

func (cs *cacheState) clearExpired(key string, now time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if latest, exists := cs.until[key]; exists && !now.Before(latest) {
		delete(cs.until, key)
	}
}

func (cs *cacheState) mark(key string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.until[key] = time.Now().Add(cs.ttl)
}

func (cs *cacheState) unmark(key string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	delete(cs.until, key)
}

func (c *Client) isCommunityMissing(channelID string) bool {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return false
	}

	return c.communityMissing.isSet(key)
}

func (c *Client) markCommunityMissing(channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}

	c.communityMissing.mark(key)
}

func (c *Client) clearCommunityMissing(channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}

	c.communityMissing.unmark(key)
}

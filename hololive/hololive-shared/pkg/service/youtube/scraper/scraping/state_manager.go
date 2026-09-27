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
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type stateStore interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

// cacheState: 채널별 boolean 캐시 상태 (in-memory + stateStore 2계층).
type cacheState struct {
	mu    sync.RWMutex
	until map[string]time.Time
	store stateStore
	ttl   time.Duration
	label string // 로그용 라벨
}

func newCacheState(store stateStore, ttl time.Duration, label string) *cacheState {
	return &cacheState{
		until: make(map[string]time.Time),
		store: store,
		ttl:   ttl,
		label: label,
	}
}

func (cs *cacheState) isSet(ctx context.Context, key, stateKey string) bool {
	now := time.Now()
	if cs.memoryStateIsSet(key, now) {
		return true
	}

	cs.clearExpiredMemoryState(key, now)

	if cs.store == nil {
		return false
	}

	return cs.hydrateFromStore(ctx, key, stateKey)
}

func (cs *cacheState) memoryStateIsSet(key string, now time.Time) bool {
	cs.mu.RLock()

	until, ok := cs.until[key]
	cs.mu.RUnlock()

	return ok && now.Before(until)
}

func (cs *cacheState) clearExpiredMemoryState(key string, now time.Time) {
	cs.mu.Lock()

	latest, exists := cs.until[key]
	if exists && !now.Before(latest) {
		delete(cs.until, key)
	}

	cs.mu.Unlock()
}

func (cs *cacheState) hydrateFromStore(ctx context.Context, key, stateKey string) bool {
	var marker bool

	if err := cs.store.Get(ctx, stateKey, &marker); err != nil {
		slog.Warn("failed to read "+cs.label+" state",
			"channel_id", key,
			"error", err)

		return false
	}

	if marker {
		cs.mu.Lock()

		cs.until[key] = time.Now().Add(cs.ttl)
		cs.mu.Unlock()

		return true
	}

	return false
}

func (cs *cacheState) mark(ctx context.Context, key, stateKey string) {
	cs.mu.Lock()

	cs.until[key] = time.Now().Add(cs.ttl)
	cs.mu.Unlock()

	if cs.store == nil {
		return
	}

	if err := cs.store.Set(ctx, stateKey, true, cs.ttl); err != nil {
		slog.Warn("failed to persist "+cs.label+" state",
			"channel_id", key,
			"error", err)
	}
}

func (cs *cacheState) unmark(ctx context.Context, key, stateKey string) {
	cs.mu.Lock()
	delete(cs.until, key)
	cs.mu.Unlock()

	if cs.store == nil {
		return
	}

	if err := cs.store.Del(ctx, stateKey); err != nil {
		slog.Warn("failed to clear "+cs.label+" state",
			"channel_id", key,
			"error", err)
	}
}

// youtube:producer 접두사는 2026-08-25 퇴역한 producer 시절 이름이지만, hololive-api와 alarm-worker의 scraping client가
// 지금도 같은 Valkey 키(분산 rate limit bucket의 기본 BucketBase, community-missing 상태, channel health,
// snapshot 간격)를 함께 쓰는 운영 식별자다. 한쪽만 바꾸면 전환 중 두 runtime이 다른 bucket과 상태를 보게 되어 rate limit
// 예산이 나뉘고 누적 상태가 사라지므로, 전환 계획 없이 접두사를 바꾸지 않는다(stack-audit 2026-09-26 C9, 파일명만 정리).
// 변경 조건: 모든 소비 runtime을 한 release로 함께 바꾸고 기존 키의 TTL 만료 대기나 1회 이전을 정한 전환 계획이 DEC로
// 확정될 때다. 이중 읽기 호환 경로는 두지 않는다.
// RSS backoff 상태(youtube:producer:video-rss-backoff:*)는 GetRecentVideos의 RSS 폴백과 함께 삭제했다
// (DEC-20260926-hololive-source-fallbacks-retirement). 남은 키는 TTL(기본 6시간)로 사라지고 읽는 코드가 없다.
const communityMissingKeyPrefix = "youtube:producer:community-missing:"

func (c *Client) communityMissingStateKey(channelID string) string {
	return communityMissingKeyPrefix + strings.TrimSpace(channelID)
}

func (c *Client) isCommunityMissing(ctx context.Context, channelID string) bool {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return false
	}

	return c.communityMissing.isSet(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) markCommunityMissing(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}

	c.communityMissing.mark(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) clearCommunityMissing(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}

	c.communityMissing.unmark(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) initStateManagers() {
	if c == nil {
		return
	}

	c.communityMissing = newCacheState(c.stateStore, c.config.CommunityMissingTTL, "community missing")

	if c.channelHealthDisabled {
		c.channelHealth = nil
		return
	}

	c.channelHealth = NewChannelHealthStore(c.stateStore, &c.channelHealthPolicy)
}

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

package scraper

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type stateStore interface {
	Get(ctx context.Context, key string, dest any) error
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

// cacheState: 채널별 boolean 캐시 상태 (in-memory + stateStore 2계층)
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

func (cs *cacheState) clear(ctx context.Context, key, stateKey string) {
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

const (
	communityMissingKeyPrefix = "youtube:scraper:community-missing:"
	videoRSSBackoffKeyPrefix  = "youtube:scraper:video-rss-backoff:"
)

func (c *Client) communityMissingStateKey(channelID string) string {
	return communityMissingKeyPrefix + strings.TrimSpace(channelID)
}

func (c *Client) videoRSSBackoffStateKey(channelID string) string {
	return videoRSSBackoffKeyPrefix + strings.TrimSpace(channelID)
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
	c.communityMissing.clear(ctx, key, c.communityMissingStateKey(key))
}

func (c *Client) isVideoRSSBackoff(ctx context.Context, channelID string) bool {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return false
	}
	return c.videoRSSBackoff.isSet(ctx, key, c.videoRSSBackoffStateKey(key))
}

func (c *Client) markVideoRSSBackoff(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.videoRSSBackoff.mark(ctx, key, c.videoRSSBackoffStateKey(key))
}

func (c *Client) clearVideoRSSBackoff(ctx context.Context, channelID string) {
	key := strings.TrimSpace(channelID)
	if key == "" {
		return
	}
	c.videoRSSBackoff.clear(ctx, key, c.videoRSSBackoffStateKey(key))
}

func (c *Client) initStateManagers() {
	if c == nil {
		return
	}
	c.communityMissing = newCacheState(c.stateStore, constants.YouTubeConfig.CommunityMissingTTL, "community missing")
	c.videoRSSBackoff = newCacheState(c.stateStore, constants.YouTubeConfig.VideoRSSBackoffTTL, "video rss backoff")
}

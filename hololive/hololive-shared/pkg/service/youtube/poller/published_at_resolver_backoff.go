package poller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type pendingPublishedAtBackoffStore interface {
	Active(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) (bool, error)
	Mark(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate, ttl time.Duration) error
	Clear(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) error
}

type cachePublishedAtBackoffStore struct {
	cache cache.Client
}

func NewCachePublishedAtBackoffStore(cacheSvc cache.Client) pendingPublishedAtBackoffStore {
	return newCachePublishedAtResolverBackoffStore(cacheSvc)
}

func newCachePublishedAtResolverBackoffStore(cacheSvc cache.Client) pendingPublishedAtBackoffStore {
	if cacheSvc == nil {
		return nil
	}
	return &cachePublishedAtBackoffStore{cache: cacheSvc}
}

func (s *cachePublishedAtBackoffStore) Active(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) (bool, error) {
	if s == nil || s.cache == nil {
		return false, nil
	}
	return s.cache.Exists(ctx, publishedAtBackoffKey(string(candidate.Kind), candidate.PostID))
}

func (s *cachePublishedAtBackoffStore) Mark(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate, ttl time.Duration) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.Set(ctx, publishedAtBackoffKey(string(candidate.Kind), candidate.PostID), "1", ttl)
}

func (s *cachePublishedAtBackoffStore) Clear(ctx context.Context, candidate trackingrepo.PublishedAtResolutionCandidate) error {
	if s == nil || s.cache == nil {
		return nil
	}
	return s.cache.Del(ctx, publishedAtBackoffKey(string(candidate.Kind), candidate.PostID))
}

func publishedAtBackoffKey(kind, postID string) string {
	return fmt.Sprintf("youtube:published_at_backoff:%s:%s", strings.TrimSpace(kind), strings.TrimSpace(postID))
}

package render

import (
	"context"
	"fmt"
	"time"
)

const (
	calendarImageCacheVersion = "v5"
	calendarImageCacheTTL     = 7 * 24 * time.Hour
)

func (r *CalendarCardRenderer) sharedCachedImage(ctx context.Context, key calendarCacheKey) ([]byte, bool) {
	if r == nil {
		return nil, false
	}

	return r.sharedCache.get(ctx, calendarImageCacheKey(key), key.entriesHash)
}

func (r *CalendarCardRenderer) storeSharedCachedImage(ctx context.Context, key calendarCacheKey, data []byte) {
	if r == nil {
		return
	}

	r.sharedCache.put(ctx, calendarImageCacheKey(key), key.entriesHash, data)
}

func calendarImageCacheKey(key calendarCacheKey) string {
	return fmt.Sprintf("bot:calendar:image:%s:%04d-%02d", calendarImageCacheVersion, key.year, key.month)
}

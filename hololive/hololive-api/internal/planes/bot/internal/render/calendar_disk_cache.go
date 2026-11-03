package render

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

const (
	calendarImageCacheVersion  = "v5"
	calendarImageCacheMaxBytes = 8 << 20
	// 월 key 하나를 항목 해시가 바뀔 때 덮어쓴다. 지난 달 key가 영구히 남지 않을 만큼만 둔다.
	calendarImageCacheTTL = 7 * 24 * time.Hour
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// sharedCachedImage는 같은 달·같은 항목 해시로 렌더한 PNG를 공유 cache에서 읽는다. 값은 항목 해시 뒤에 PNG를
// 붙인 형태라 해시가 다르면(같은 달 이전 항목) miss로 보고 다시 렌더한다. 읽기 중 cache 오류는 miss로 보고
// 렌더 실패로 올리지 않는다.
func (r *CalendarCardRenderer) sharedCachedImage(ctx context.Context, key calendarCacheKey) ([]byte, bool) {
	if r == nil || r.sharedCache == nil {
		return nil, false
	}

	cacheKey := calendarImageCacheKey(key)

	data, ok, err := r.sharedCache.Get(ctx, cacheKey)
	if err != nil {
		logCalendarImageCacheFailure("read", cacheKey, err)

		return nil, false
	}

	if !ok || len(data) > len(key.entriesHash)+calendarImageCacheMaxBytes {
		return nil, false
	}

	image, found := bytes.CutPrefix(data, []byte(key.entriesHash))
	if !found || !isPNGData(image) {
		return nil, false
	}

	return image, true
}

// storeSharedCachedImage는 같은 달 key를 덮어써 이전 항목 해시의 이미지를 대체한다.
func (r *CalendarCardRenderer) storeSharedCachedImage(ctx context.Context, key calendarCacheKey, data []byte) {
	if r == nil || r.sharedCache == nil || !isPNGData(data) || len(data) > calendarImageCacheMaxBytes {
		return
	}

	cacheKey := calendarImageCacheKey(key)
	value := slices.Concat([]byte(key.entriesHash), data)

	if err := r.sharedCache.Set(ctx, cacheKey, value, calendarImageCacheTTL); err != nil {
		logCalendarImageCacheFailure("write", cacheKey, err)
	}
}

func logCalendarImageCacheFailure(operation, key string, err error) {
	slog.Default().Warn("calendar_image_cache_failed",
		slog.String("operation", operation),
		privacylog.CacheKeyAttr(key),
		slog.Any("error", err),
	)
}

func calendarImageCacheKey(key calendarCacheKey) string {
	return fmt.Sprintf("bot:calendar:image:%s:%04d-%02d", calendarImageCacheVersion, key.year, key.month)
}

func isPNGData(data []byte) bool {
	return bytes.HasPrefix(data, pngSignature)
}

package render

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

const (
	calendarImageCacheVersion = "v7"
	calendarImageCacheTTL     = 7 * 24 * time.Hour
)

func (r *CalendarCardRenderer) sharedCachedAlbum(ctx context.Context, key calendarCacheKey) ([][]byte, []byte, bool) {
	if r == nil {
		return nil, nil, false
	}

	hash := sha256.Sum256([]byte(key.entriesHash))
	record, ok := r.sharedCache.read(ctx, calendarAlbumCacheKey(key))

	if !ok || len(record) <= len(hash) || !bytes.Equal(record[:len(hash)], hash[:]) {
		return nil, nil, false
	}

	body := record[len(hash):]
	pages, valid := decodeSchedulePages(body)

	if !valid {
		return nil, nil, false
	}

	return cloneSchedulePages(pages), body, true
}

func (r *CalendarCardRenderer) storeSharedCachedAlbum(ctx context.Context, key calendarCacheKey, pages [][]byte) {
	if r == nil {
		return
	}

	hash := sha256.Sum256([]byte(key.entriesHash))
	if record := encodeSchedulePages(hash, pages); record != nil {
		r.sharedCache.write(ctx, calendarAlbumCacheKey(key), record)
	}
}

func calendarAlbumCacheKey(key calendarCacheKey) string {
	kind := cmp.Or(string(key.kind), string(calendarPageEmpty))
	theme := cmp.Or(key.theme, CalendarThemePrismDaylight)

	return fmt.Sprintf("bot:calendar:album:%s:%04d-%02d:%s:%s", calendarImageCacheVersion, key.year, key.month, kind, theme)
}

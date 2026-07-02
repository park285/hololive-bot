package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const calendarImageCacheLimit = 24

type calendarCacheKey struct {
	month       int
	year        int
	entriesHash string
}

func (key calendarCacheKey) string() string {
	return fmt.Sprintf("%04d-%02d-%s", key.year, key.month, key.entriesHash)
}

func (r *CalendarCardRenderer) cachedImages(key calendarCacheKey) ([][]byte, bool) {
	if r == nil {
		return nil, false
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	pages, ok := r.cache[key]
	if !ok {
		return nil, false
	}
	return clonePages(pages), true
}

func (r *CalendarCardRenderer) storeCachedImages(key calendarCacheKey, pages [][]byte) {
	if r == nil {
		return
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	if r.cache == nil {
		r.cache = make(map[calendarCacheKey][][]byte)
	}
	if _, ok := r.cache[key]; !ok {
		r.cacheOrder = append(r.cacheOrder, key)
	}
	r.cache[key] = clonePages(pages)
	for len(r.cacheOrder) > calendarImageCacheLimit {
		oldest := r.cacheOrder[0]
		r.cacheOrder = r.cacheOrder[1:]
		delete(r.cache, oldest)
	}
}

func clonePages(pages [][]byte) [][]byte {
	cloned := make([][]byte, len(pages))
	for i, page := range pages {
		cloned[i] = bytes.Clone(page)
	}
	return cloned
}

func newCalendarCacheKey(month, year int, entries []domain.CalendarEntry) calendarCacheKey {
	hash := sha256.New()
	writeCacheInt(hash, year)
	writeCacheInt(hash, month)
	writeCacheInt(hash, len(entries))
	for _, entry := range entries {
		writeCacheInt(hash, entry.Day)
		writeCacheString(hash, string(entry.Kind))
		writeCacheInt(hash, entry.Ordinal)
		writeMemberCacheHash(hash, entry.Member)
	}
	return calendarCacheKey{
		month:       month,
		year:        year,
		entriesHash: hex.EncodeToString(hash.Sum(nil)),
	}
}

func writeMemberCacheHash(w io.Writer, member *domain.Member) {
	if member == nil {
		writeCacheString(w, "<nil>")
		return
	}
	writeCacheInt(w, member.ID)
	writeCacheString(w, member.ChannelID)
	writeCacheString(w, member.Name)
	writeCacheString(w, member.NameKo)
	writeCacheString(w, member.ShortKoreanName)
	writeCacheString(w, member.Photo)
}

func writeCacheInt(w io.Writer, value int) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], cacheUint64(value))
	if _, err := w.Write(buf[:]); err != nil {
		return
	}
}

func writeCacheString(w io.Writer, value string) {
	writeCacheInt(w, len(value))
	if _, err := io.WriteString(w, value); err != nil {
		return
	}
}

func cacheUint64(value int) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"slices"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const calendarImageCacheLimit = 24

type calendarCacheKey struct {
	month       int
	year        int
	entriesHash string
}

type imageCache[K comparable] struct {
	mu    sync.Mutex
	limit int
	data  map[K][]byte
	order []K
}

func newImageCache[K comparable](limit int) *imageCache[K] {
	return &imageCache[K]{limit: limit, data: make(map[K][]byte)}
}

func (c *imageCache[K]) get(key K) ([]byte, bool) {
	data, ok := c.getImmutable(key)

	return bytes.Clone(data), ok
}

// getImmutable의 결과는 교체되거나 퇴거된 뒤에도 읽기 전용이다.
func (c *imageCache[K]) getImmutable(key K) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, ok := c.data[key]
	if !ok {
		return nil, false
	}

	c.touch(key)

	return data, true
}

func (c *imageCache[K]) put(key K, data []byte) {
	c.putImmutable(key, bytes.Clone(data))
}

// putImmutable 이후 호출자도 data를 수정하면 안 된다.
func (c *imageCache[K]) putImmutable(key K, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.data[key]; ok {
		c.touch(key)
	} else {
		c.order = append(c.order, key)
	}

	c.data[key] = data
	for len(c.order) > c.limit {
		delete(c.data, c.order[0])

		c.order = slices.Delete(c.order, 0, 1)
	}
}

func (c *imageCache[K]) touch(key K) {
	if i := slices.Index(c.order, key); i >= 0 {
		c.order = append(slices.Delete(c.order, i, i+1), key)
	}
}

func (r *CalendarCardRenderer) cachedImage(key calendarCacheKey) ([]byte, bool) {
	if r == nil {
		return nil, false
	}

	return r.cache.get(key)
}

func (r *CalendarCardRenderer) storeCachedImage(key calendarCacheKey, data []byte) {
	if r == nil {
		return
	}

	r.cache.put(key, data)
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

package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
)

func (r *ScheduleCardRenderer) cachedPages(key scheduleCardKey) ([][]byte, bool) {
	body, ok := r.cache.get(key)
	if !ok {
		return nil, false
	}

	return decodeSchedulePages(body)
}

// 공유 값은 입력 해시, 전체 바이트 수, 페이지 수, (길이, PNG) 순서다. 메모리 캐시는 키 자체로 입력을 식별한다.
func encodeSchedulePages(hash [sha256.Size]byte, pages [][]byte) []byte {
	if len(pages) == 0 || len(pages) > schedulecard.MaxPages {
		return nil
	}

	size := len(hash) + 16 + 8*len(pages)
	for _, page := range pages {
		if len(page) > sharedPNGCacheMaxBytes-size {
			return nil
		}

		size += len(page)
	}

	record := make([]byte, 0, size)

	record = append(record, hash[:]...)
	record = binary.BigEndian.AppendUint64(record, cacheUint64(size))
	record = binary.BigEndian.AppendUint64(record, uint64(len(pages)))

	for _, page := range pages {
		record = binary.BigEndian.AppendUint64(record, uint64(len(page)))
		record = append(record, page...)
	}

	return record
}

func decodeSchedulePages(body []byte) ([][]byte, bool) {
	if len(body) < 16 || len(body) > sharedPNGCacheMaxBytes-sha256.Size || binary.BigEndian.Uint64(body) != uint64(len(body))+sha256.Size {
		return nil, false
	}

	count := binary.BigEndian.Uint64(body[8:])
	if count == 0 || count > schedulecard.MaxPages {
		return nil, false
	}

	body = body[16:]

	pages := make([][]byte, 0, count)

	for range count {
		if len(body) < 8 {
			return nil, false
		}

		size := binary.BigEndian.Uint64(body)

		body = body[8:]

		if size > uint64(len(body)) || !isPNGData(body[:size]) {
			return nil, false
		}

		pages = append(pages, body[:size:size])
		body = body[size:]
	}

	if len(body) != 0 {
		return nil, false
	}

	return pages, true
}

func cloneSchedulePages(pages [][]byte) [][]byte {
	cloned := make([][]byte, len(pages))
	for i, page := range pages {
		cloned[i] = bytes.Clone(page)
	}

	return cloned
}

package template

import (
	"text/template"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// templateCacheMaxEntries는 채널 override와 이전 버전의 파싱 결과를 함께 제한합니다.
const templateCacheMaxEntries = 256

type cacheKey struct {
	templateKey domain.TemplateKey
	channelID   string
	id          int64
	version     int64
}

type cacheEntry struct {
	tmpl     *template.Template
	storedAt time.Time
}

func (r *Renderer) storeTemplateAt(ck cacheKey, tmpl *template.Template, now time.Time) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.storeTemplateLocked(ck, tmpl, now)
}

func (r *Renderer) storeTemplateLocked(ck cacheKey, tmpl *template.Template, now time.Time) {
	if _, exists := r.cache[ck]; !exists {
		for len(r.cache) >= templateCacheMaxEntries {
			if !r.evictOldestLocked() {
				break
			}
		}
	}

	r.cache[ck] = cacheEntry{tmpl: tmpl, storedAt: now}
}

func (r *Renderer) evictOldestLocked() bool {
	var (
		oldestKey cacheKey
		oldestAt  time.Time
	)

	found := false

	for ck, entry := range r.cache {
		if !found || entry.storedAt.Before(oldestAt) {
			oldestKey, oldestAt, found = ck, entry.storedAt, true
		}
	}

	if !found {
		return false
	}

	delete(r.cache, oldestKey)

	return true
}

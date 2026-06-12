package template

import (
	"text/template"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// templateCacheMaxEntries 는 cache 의 size-bound. 키가 (templateKey, channelID) 조합이라
// 채널 수에 비례해 unbounded 성장 위험이 있다.
const templateCacheMaxEntries = 256

type cacheKey struct {
	templateKey domain.TemplateKey
	channelID   string
}

type cacheEntry struct {
	tmpl     *template.Template
	storedAt time.Time
}

func (r *Renderer) storeTemplateAt(ck cacheKey, tmpl *template.Template, now time.Time) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
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
	var oldestKey cacheKey
	var oldestAt time.Time
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

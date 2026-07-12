package pollers

import (
	"strings"
	"sync"
	"time"
)

const (
	publicationFreshnessHorizon     = 72 * time.Hour
	publicationFreshnessFutureSkew  = time.Hour
	publicationFreshnessMaxAttempts = 3
)

type freshnessDeferrals struct {
	mu       sync.Mutex
	attempts map[string]int
}

func newFreshnessDeferrals() *freshnessDeferrals {
	return &freshnessDeferrals{attempts: make(map[string]int)}
}

func (d *freshnessDeferrals) recordFailure(channelID, videoID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := channelID + "|" + videoID
	d.attempts[key]++
	return d.attempts[key]
}

func (d *freshnessDeferrals) clear(channelID, videoID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.attempts, channelID+"|"+videoID)
}

// 목록에서 사라진 보류 항목도 상한까지 시도 횟수를 올리며 붙잡는다. 바로 지우면
// watermark가 전진해, 항목이 watermark 아래로 재등장할 때 영구 유실된다.
func (d *freshnessDeferrals) reconcileChannel(
	channelID string,
	scrapedVideoIDs map[string]struct{},
	deferredVideoIDs map[string]struct{},
) (holdWatermark bool, departed []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	prefix := channelID + "|"
	for key, attempts := range d.attempts {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		videoID := strings.TrimPrefix(key, prefix)
		if _, stillDeferred := deferredVideoIDs[videoID]; stillDeferred {
			holdWatermark = true
			continue
		}
		if _, scraped := scrapedVideoIDs[videoID]; scraped {
			delete(d.attempts, key)
			continue
		}
		attempts++
		if attempts >= publicationFreshnessMaxAttempts {
			delete(d.attempts, key)
			departed = append(departed, videoID)
			continue
		}
		d.attempts[key] = attempts
		holdWatermark = true
	}
	return holdWatermark, departed
}

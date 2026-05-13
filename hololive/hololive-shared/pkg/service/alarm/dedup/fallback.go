// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dedup

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// fallbackEntry: 로컬 dedup 엔트리
type fallbackEntry struct {
	expiresAtUnixNano int64
}

func (e fallbackEntry) isExpired(now time.Time) bool {
	return now.UnixNano() >= e.expiresAtUnixNano
}

type LocalFallback struct {
	logger   *slog.Logger
	keys     sync.Map
	keyCount atomic.Int64
	now      func() time.Time
}

type fallbackClaimResult struct {
	done     bool
	acquired bool
}

func NewLocalFallback(logger ...*slog.Logger) *LocalFallback {
	log := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		log = logger[0]
	}

	return &LocalFallback{
		logger: log,
		now:    time.Now,
	}
}

func (f *LocalFallback) TryClaimOnOutage(key string, ttl time.Duration, err error) bool {
	normalizedTTL := normalizeFallbackTTL(ttl)
	acquired := f.tryClaim(key, normalizedTTL)

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	f.logger.Warn("SETNX claim 실패, 로컬 폴백 사용",
		slog.String("key", key),
		slog.Bool("fallback_acquired", acquired),
		slog.String("error", errMsg),
	)
	return acquired
}

func (f *LocalFallback) ReleaseClaims(claimKeys []string) {
	for _, key := range claimKeys {
		if _, loaded := f.keys.LoadAndDelete(key); loaded {
			f.keyCount.Add(-1)
		}
	}
}

// tryClaim: 로컬 dedup claim 시도
func (f *LocalFallback) tryClaim(key string, ttl time.Duration) bool {
	now := f.now()
	expiresAt := now.Add(ttl).UnixNano()

	// 용량 초과 시 만료 항목 정리
	if f.keyCount.Load() >= int64(constants.LocalFallbackCleanupMaxKeys) {
		f.cleanupExpired(now)
	}

	newEntry := fallbackEntry{expiresAtUnixNano: expiresAt}

	for {
		result := f.tryClaimOnce(key, now, newEntry)
		if result.done {
			return result.acquired
		}
	}
}

func (f *LocalFallback) tryClaimOnce(key string, now time.Time, newEntry fallbackEntry) fallbackClaimResult {
	loadedEntry, exists := f.keys.Load(key)
	if !exists {
		return fallbackClaimResult{done: f.tryStoreNewClaim(key, newEntry), acquired: true}
	}

	entry, ok := loadedEntry.(fallbackEntry)
	if !ok {
		f.deleteCorruptEntry(key, loadedEntry)
		return fallbackClaimResult{}
	}

	if !entry.isExpired(now) {
		return fallbackClaimResult{done: true}
	}

	return fallbackClaimResult{
		done:     f.keys.CompareAndSwap(key, entry, newEntry),
		acquired: true,
	}
}

func (f *LocalFallback) tryStoreNewClaim(key string, newEntry fallbackEntry) bool {
	if _, loaded := f.keys.LoadOrStore(key, newEntry); loaded {
		return false
	}
	f.keyCount.Add(1)
	return true
}

func (f *LocalFallback) deleteCorruptEntry(key any, value any) {
	if f.keys.CompareAndDelete(key, value) {
		f.keyCount.Add(-1)
	}
}

// cleanupExpired: 만료된 항목 정리 (잠금 보유 상태에서 호출)
func (f *LocalFallback) cleanupExpired(now time.Time) {
	f.keys.Range(func(key, value any) bool {
		entry, ok := value.(fallbackEntry)
		if !ok {
			f.deleteCorruptEntry(key, value)
			return true
		}

		if entry.isExpired(now) {
			f.deleteExpiredEntry(key, entry)
		}
		return true
	})
}

func (f *LocalFallback) deleteExpiredEntry(key any, entry fallbackEntry) {
	if f.keys.CompareAndDelete(key, entry) {
		f.keyCount.Add(-1)
	}
}

// normalizeFallbackTTL: TTL 정규화 (0 또는 최대치 초과 시 기본값 적용)
func normalizeFallbackTTL(ttl time.Duration) time.Duration {
	if ttl == 0 || ttl > constants.LocalFallbackDedupTTL {
		return constants.LocalFallbackDedupTTL
	}
	return ttl
}

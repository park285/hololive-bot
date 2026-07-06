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

package messagestrings

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FallbackSentinel은 message_strings/notification_templates 로딩 자체가 실패하는 최후의
// 경우에만 쓰는, 유일하게 허용된 하드코딩 사용자-facing 문자열이다. 메시지 "콘텐츠"가 아니라
// DB 장애 sentinel이다(에러 문구마저 DB에 있어, DB가 죽으면 보여줄 문구도 못 읽는 chicken-egg 방어).
const FallbackSentinel = "요청 처리 중 오류가 발생했습니다. 잠시 후 다시 시도해주세요."

const (
	NamespaceOrg         = "org"
	NamespaceAlarmType   = "alarmtype"
	NamespaceNewsCat     = "newscat"
	NamespaceSocial      = "social"
	NamespaceMisc        = "misc"
	NamespaceError       = "error"
	NamespaceNotify      = "notify"
	NamespaceCalendar    = "calendar"
	NamespaceLiveCard    = "livecard"
	NamespaceProfileCard = "profilecard"
	NamespaceRankCard    = "rankcard"
	NamespaceTimeFmt     = "timefmt"
	NamespaceKaring      = "karing"
)

const (
	lazyLoadRetryInterval = 30 * time.Second
	lazyLoadTimeout       = 5 * time.Second
)

type queryRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Store struct {
	pool        queryRunner
	logger      *slog.Logger
	mu          sync.RWMutex
	cache       map[string]map[string]string
	loaded      bool
	reloadMu    sync.Mutex
	nextRetryAt time.Time
	loadTimeout time.Duration
}

func NewStore(pool *pgxpool.Pool, logger *slog.Logger) *Store {
	return &Store{pool: pool, logger: logger, loadTimeout: lazyLoadTimeout}
}

func (s *Store) Load(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.reload(ctx)
}

func (s *Store) Get(namespace, key string) string {
	return s.GetContext(context.Background(), namespace, key)
}

func (s *Store) GetContext(ctx context.Context, namespace, key string) string {
	if s == nil {
		return ""
	}
	s.ensureLoaded(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if values, ok := s.cache[namespace]; ok {
		return values[key]
	}
	return ""
}

func (s *Store) GetOr(namespace, key, fallback string) string {
	if v := s.Get(namespace, key); v != "" {
		return v
	}
	return fallback
}

func (s *Store) GetOrContext(ctx context.Context, namespace, key, fallback string) string {
	if v := s.GetContext(ctx, namespace, key); v != "" {
		return v
	}
	return fallback
}

func (s *Store) VTuberFallbackContext(ctx context.Context) string {
	if v := s.GetContext(ctx, NamespaceMisc, "vtuber_fallback"); v != "" {
		return v
	}
	return "VTuber"
}

func (s *Store) GetMap(namespace string) map[string]string {
	if s == nil {
		return nil
	}
	s.ensureLoaded(context.Background())

	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.cache[namespace]
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	maps.Copy(out, src)
	return out
}

func (s *Store) Invalidate() {
	s.mu.Lock()
	s.loaded = false
	s.cache = nil
	s.nextRetryAt = time.Time{}
	s.mu.Unlock()
}

func (s *Store) ensureLoaded(ctx context.Context) {
	s.mu.RLock()
	loaded := s.loaded
	s.mu.RUnlock()
	if loaded {
		return
	}

	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	s.mu.RLock()
	loaded = s.loaded
	retryAt := s.nextRetryAt
	s.mu.RUnlock()
	if loaded {
		return
	}
	if !retryAt.IsZero() && time.Now().Before(retryAt) {
		return
	}

	timeout := s.loadTimeout
	if timeout <= 0 {
		timeout = lazyLoadTimeout
	}
	loadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := s.reload(loadCtx); err != nil {
		s.mu.Lock()
		s.nextRetryAt = time.Now().Add(lazyLoadRetryInterval)
		s.mu.Unlock()
		s.warn(ctx, "messagestrings: lazy load failed", "error", err)
	}
}

func (s *Store) reload(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, mustSQL("store_0189_01.sql"))
	if err != nil {
		return fmt.Errorf("query message_strings: %w", err)
	}
	defer rows.Close()

	next := make(map[string]map[string]string)
	for rows.Next() {
		var namespace, key, value string
		if scanErr := rows.Scan(&namespace, &key, &value); scanErr != nil {
			return fmt.Errorf("scan message_strings: %w", scanErr)
		}
		values, ok := next[namespace]
		if !ok {
			values = make(map[string]string)
			next[namespace] = values
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate message_strings: %w", err)
	}

	s.mu.Lock()
	s.cache = next
	s.loaded = true
	s.nextRetryAt = time.Time{}
	s.mu.Unlock()
	return nil
}

func (s *Store) warn(ctx context.Context, msg string, args ...any) {
	if s.logger != nil {
		s.logger.WarnContext(ctx, msg, args...)
	}
}

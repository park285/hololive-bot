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
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
)

type queryRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store는 message_strings(DB 정본)를 기동 때 한 번 적재해 두고 조회한다.
// DEC-20260926-hololive-message-strings-startup-validation: 운영 중 쓰기 경로와 재적재 호출자가 없으므로
// 조회 시 lazy 재적재와 코드 대체 문구를 두지 않는다. 각 runtime은 기동 때 Load와 Validate를 호출하고,
// 실패하면 기동에 실패한다.
type Store struct {
	pool   queryRunner
	logger *slog.Logger
	mu     sync.RWMutex
	cache  map[string]map[string]string
	loaded bool
}

func NewStore(pool *pgxpool.Pool, logger *slog.Logger) *Store {
	initMetrics()

	return &Store{pool: pool, logger: logger}
}

// Load는 message_strings 전체를 적재한다. 실패하면 이전 적재 상태를 바꾸지 않고 오류를 돌려준다.
func (s *Store) Load(ctx context.Context) error {
	if s == nil {
		return errors.New("message strings store is nil")
	}

	if err := s.load(ctx); err != nil {
		observeLoadFailure()

		return fmt.Errorf("load message strings: %w", err)
	}

	return nil
}

// Requirements는 runtime이 기동 때 확인할 message_strings 계약이다. Keys는 값이 비어 있지 않아야 하고,
// Namespaces는 동적 key로 조회하는 namespace라 적어도 한 행이 있어야 한다.
type Requirements struct {
	Keys       []Key
	Namespaces []string
}

// Validate는 적재된 값이 요구 key와 namespace를 모두 갖췄는지 확인한다. 누락을 모두 모아 한 오류로 돌려준다.
func (s *Store) Validate(requirements Requirements) error {
	if s == nil {
		return errors.New("message strings store is nil")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.loaded {
		return errors.New("message strings are not loaded")
	}

	var missing []string

	for _, key := range requirements.Keys {
		if strings.TrimSpace(s.cache[key.Namespace][key.Name]) == "" {
			missing = append(missing, key.String())
		}
	}

	for _, namespace := range requirements.Namespaces {
		if len(s.cache[namespace]) == 0 {
			missing = append(missing, namespace+"/*")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("message strings missing required entries: %s", strings.Join(missing, ", "))
	}

	return nil
}

// Text는 기동 검증을 거친 key의 값을 돌려준다. 값이 없으면 빈 문자열과 함께 lookup metric을 남긴다.
// 검증된 key에서는 일어나지 않으므로 metric이 오르면 검증 목록 누락이나 Load 누락 같은 조립 결함이다.
func (s *Store) Text(key Key) string {
	value, _ := s.Lookup(key.Namespace, key.Name)

	return value
}

// Lookup은 동적 key(조직명, 뉴스 분류, 알람 종류 등)를 조회한다. 없으면 ("", false)이며 호출자가 원문을 쓴다.
func (s *Store) Lookup(namespace, key string) (string, bool) {
	if s == nil {
		observeLookupMiss(lookupMissReasonUnloaded, namespace)

		return "", false
	}

	s.mu.RLock()

	loaded := s.loaded
	value := s.cache[namespace][key]
	s.mu.RUnlock()

	if value != "" {
		return value, true
	}

	reason := lookupMissReasonMissing

	if !loaded {
		reason = lookupMissReasonUnloaded
	}

	observeLookupMiss(reason, namespace)

	return "", false
}

func (s *Store) GetMap(namespace string) map[string]string {
	if s == nil {
		return nil
	}

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

func (s *Store) load(ctx context.Context) error {
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
	s.mu.Unlock()

	return nil
}

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
	"context"
	"log/slog"
	"sync"
	"time"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

type LogicalScheduleNotifiedData struct {
	StreamID       string `json:"stream_id"`
	StartScheduled string `json:"start_scheduled"`
	NotifiedAt     string `json:"notified_at"`
}

type Service struct {
	cache           cache.Client
	targetPolicy    sharedchecker.TargetMinutePolicy
	targetMinutesMu sync.RWMutex
	fallback        *LocalFallback
	logger          *slog.Logger
}

type notifiedDataSource int

const (
	notifiedDataSourceMissing notifiedDataSource = iota
	notifiedDataSourceHash
	notifiedDataSourceLegacyString
)

func NewService(c cache.Client, targetMinutes []int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cache:        c,
		targetPolicy: sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes)),
		fallback:     NewLocalFallback(logger),
		logger:       logger,
	}
}

// UpdateTargetMinutes는 runtime target minute 정책을 원자적으로 교체한다.
func (s *Service) UpdateTargetMinutes(targetMinutes []int) {
	s.targetMinutesMu.Lock()
	defer s.targetMinutesMu.Unlock()

	s.targetPolicy = sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes))
}

// tryClaimKey: SETNX 기반 키 선점 (Valkey 장애 시 로컬 폴백)
func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) bool {
	acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
	if err != nil {
		s.logger.Debug("dedup claim fallback",
			slog.String("key", key),
			slog.String("error", err.Error()),
		)
		return s.fallback.TryClaimOnOutage(key, ttl, err)
	}
	s.logger.Debug("dedup claim result",
		slog.String("key", key),
		slog.Bool("acquired", acquired),
	)
	return acquired
}

func (s *Service) targetMinutesSnapshot() []int {
	return s.targetPolicySnapshot().Clone()
}

func (s *Service) TargetMinutesSnapshot() []int {
	return s.targetMinutesSnapshot()
}

func (s *Service) targetPolicySnapshot() sharedchecker.TargetMinutePolicy {
	s.targetMinutesMu.RLock()
	defer s.targetMinutesMu.RUnlock()

	return s.targetPolicy
}

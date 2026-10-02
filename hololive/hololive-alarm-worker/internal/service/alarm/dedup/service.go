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
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/privacylog"
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
	logger          *slog.Logger
}

func NewService(c cache.Client, targetMinutes []int, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cache:        c,
		targetPolicy: sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes)),
		logger:       logger,
	}
}

// UpdateTargetMinutes는 runtime target minute 정책을 원자적으로 교체한다.
func (s *Service) UpdateTargetMinutes(targetMinutes []int) {
	s.targetMinutesMu.Lock()
	defer s.targetMinutesMu.Unlock()

	s.targetPolicy = sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(targetMinutes))
}

// SETNX 기반 키 선점. 저장소 오류는 "이미 선점됨"(false, nil)과 구분해 오류로 돌려준다.
// 호출자가 이를 실패(sendOutcomeFailed)로 기록해야 Valkey 장애 동안 알림이 skip으로 사라지지 않는다.
// 선점 key에는 room 식별자가 보간되므로 오류와 로그에는 가명 토큰만 남긴다.
func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
	if err != nil {
		return false, fmt.Errorf("dedup claim setnx: claim_key_token=%s: %w", privacylog.Pseudonym(key), err)
	}

	s.logger.Debug("dedup claim result",
		slog.String("claim_key_token", privacylog.Pseudonym(key)),
		slog.Bool("acquired", acquired),
	)

	return acquired, nil
}

func (s *Service) TargetMinutesSnapshot() []int {
	return s.targetPolicySnapshot().Clone()
}

func (s *Service) targetPolicySnapshot() sharedchecker.TargetMinutePolicy {
	s.targetMinutesMu.RLock()
	defer s.targetMinutesMu.RUnlock()

	return s.targetPolicy
}

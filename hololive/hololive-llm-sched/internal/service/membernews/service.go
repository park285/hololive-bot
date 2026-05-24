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

package membernews

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/pkg/stringutil"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Service struct {
	repository      *Repository
	summarizer      model.Summarizer
	sourceValidator *SourceValidator
	membersData     domain.MemberDataProvider
	logger          *slog.Logger
	now             func() time.Time
}

func NewService(
	repository *Repository,
	summarizer model.Summarizer,
	sourceValidator *SourceValidator,
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repository:      repository,
		summarizer:      summarizer,
		sourceValidator: sourceValidator,
		membersData:     membersData,
		logger:          logger,
		now:             time.Now,
	}
}

func (s *Service) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}
	s.now = clockFn
}

func (s *Service) GenerateRoomDigest(ctx context.Context, roomID string, period model.Period) (*model.Digest, error) {
	if err := s.validateGenerateRoomDigestRequest(roomID); err != nil {
		return nil, err
	}

	normalizedPeriod := model.NormalizePeriod(period)
	members, err := s.repository.GetRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room members: %w", err)
	}
	if len(members) == 0 {
		return nil, model.ErrNoSubscribedMembers
	}

	candidates, err := s.repository.ListActiveMajorEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active major events: %w", err)
	}

	memberProvider := s.membersData
	if memberProvider != nil {
		memberProvider = memberProvider.WithContext(ctx)
	}

	filtered := FilterCandidates(candidates, normalizedPeriod, s.now(), members, memberProvider, s.sourceValidator)
	if len(filtered) == 0 {
		return emptyDigest(normalizedPeriod), nil
	}

	digest := s.summarizeRoomDigest(ctx, roomID, normalizedPeriod, members, filtered)
	normalizeDigest(digest, normalizedPeriod, len(filtered))
	return digest, nil
}

func (s *Service) validateGenerateRoomDigestRequest(roomID string) error {
	if s == nil {
		return fmt.Errorf("membernews service is nil")
	}
	if s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if stringutil.TrimSpace(roomID) == "" {
		return fmt.Errorf("room id is required")
	}
	return nil
}

func emptyDigest(period model.Period) *model.Digest {
	return &model.Digest{
		ResultType:   sharedmodel.SummaryResultEmpty,
		Period:       period,
		Headline:     model.DefaultHeadline(period),
		TopItems:     []model.SummaryItem{},
		MoreSummary:  "",
		OmittedCount: 0,
		TotalCount:   0,
	}
}

func (s *Service) summarizeRoomDigest(ctx context.Context, roomID string, period model.Period, members []string, filtered []model.FilteredCandidate) *model.Digest {
	if s.summarizer == nil {
		digest := BuildDeterministicFallback(period, filtered)
		digest.TotalCount = len(filtered)
		return digest
	}

	digest, err := s.summarizer.Summarize(ctx, model.SummarizeInput{
		Period:      period,
		Now:         s.now(),
		RoomID:      roomID,
		RoomMembers: members,
		Candidates:  filtered,
	})
	if err != nil {
		s.logger.Warn("Member news summarize failed, using deterministic fallback",
			slog.String("room_id", roomID),
			slog.String("period", string(period)),
			slog.String("error", err.Error()),
		)
		digest = BuildDeterministicFallback(period, filtered)
	}

	if digest == nil || len(digest.TopItems) == 0 {
		digest = BuildDeterministicFallback(period, filtered)
	}

	return digest
}

func normalizeDigest(digest *model.Digest, period model.Period, totalCount int) {
	digest.Period = period
	if digest.Headline == "" {
		digest.Headline = model.DefaultHeadline(period)
	}
	if digest.OmittedCount < 0 {
		digest.OmittedCount = 0
	}
	digest.TotalCount = totalCount
}

func (s *Service) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.Subscribe(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("subscribe room: %w", err)
	}
	return nil
}

func (s *Service) UnsubscribeRoom(ctx context.Context, roomID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.Unsubscribe(ctx, roomID); err != nil {
		return fmt.Errorf("unsubscribe room: %w", err)
	}
	return nil
}

func (s *Service) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, fmt.Errorf("membernews repository is nil")
	}
	subscribed, err := s.repository.IsSubscribed(ctx, roomID)
	if err != nil {
		return false, fmt.Errorf("check room subscription: %w", err)
	}
	return subscribed, nil
}

func (s *Service) ListSubscribedRooms(ctx context.Context) ([]model.SubscribedRoom, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("membernews repository is nil")
	}
	rooms, err := s.repository.ListSubscribedRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscribed rooms: %w", err)
	}
	return rooms, nil
}

func (s *Service) WarmupSubscriptionCache(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.WarmupCacheFromDB(ctx); err != nil {
		return fmt.Errorf("warmup subscription cache: %w", err)
	}
	return nil
}

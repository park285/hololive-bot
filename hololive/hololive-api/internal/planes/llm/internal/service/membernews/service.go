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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/promptguard"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	newssummarizer "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/summarizer"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Service struct {
	repository      *Repository
	summarizer      model.Summarizer
	sourceValidator *SourceValidator
	promptGuard     *promptguard.Guard
	membersData     domain.MemberDataProvider
	logger          *slog.Logger
	now             func() time.Time
}

type ServiceOption func(*Service)

func WithPromptGuard(guard *promptguard.Guard) ServiceOption {
	return func(service *Service) {
		service.promptGuard = guard
	}
}

func NewService(
	repository *Repository,
	summarizer model.Summarizer,
	sourceValidator *SourceValidator,
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
	opts ...ServiceOption,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	service := &Service{
		repository:      repository,
		summarizer:      summarizer,
		sourceValidator: sourceValidator,
		membersData:     membersData,
		logger:          logger,
		now:             time.Now,
	}

	for _, opt := range opts {
		opt(service)
	}

	return service
}

func (s *Service) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}

	s.now = clockFn
}

func (s *Service) GenerateRoomDigest(ctx context.Context, roomID string, period model.Period) (*model.Digest, error) {
	if err := s.validateGenerateRoomDigestRequest(roomID); err != nil {
		return nil, fmt.Errorf("validate generate room digest request: %w", err)
	}

	normalizedPeriod := model.NormalizePeriod(period)

	members, err := s.repository.GetRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room members: %w", err)
	}

	if len(members) == 0 {
		return nil, model.ErrNoSubscribedMembers
	}

	now := s.now()

	prepared, err := s.prepareCandidates(ctx, normalizedPeriod, now)
	if err != nil {
		return nil, fmt.Errorf("list active major events: %w", err)
	}

	return s.generatePreparedRoomDigest(ctx, roomID, normalizedPeriod, now, members, prepared)
}

func (s *Service) generatePreparedRoomDigest(ctx context.Context, roomID string, period model.Period, now time.Time, members []string, prepared *filter.PreparedCandidates) (*model.Digest, error) {
	filtered, err := prepared.Filter(ctx, members, s.membersData, s.sourceValidator)
	if err != nil {
		return nil, fmt.Errorf("filter member news candidates: %w", err)
	}

	filtered, err = filterPromptCandidates(filtered, s.promptGuard, s.logger)
	if err != nil {
		return nil, fmt.Errorf("guard member news candidates: %w", err)
	}

	if len(filtered) == 0 {
		return emptyDigest(period), nil
	}

	digest, err := s.summarizeRoomDigest(ctx, roomID, period, members, filtered, now)
	if err != nil {
		return nil, fmt.Errorf("summarize room digest: %w", err)
	}

	normalizeDigest(digest, period, len(filtered))

	return digest, nil
}

func (s *Service) validateGenerateRoomDigestRequest(roomID string) error {
	if s == nil {
		return errors.New("membernews service is nil")
	}

	if s.repository == nil {
		return errors.New("membernews repository is nil")
	}

	if stringutil.TrimSpace(roomID) == "" {
		return errors.New("room id is required")
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

// summarizeRoomDigest는 결정적 fallback digest를 만드는 유일한 곳이다. 요약기(summarizer)는 실패를 오류로만 알리고,
// 여기서 사유를 bounded enum으로 분류해 result_type과 함께 metric과 로그에 남긴다(stack audit B5).
// 호출자 context가 끝났으면 fallback digest를 만들지 않고 취소를 돌려준다.
func (s *Service) summarizeRoomDigest(ctx context.Context, roomID string, period model.Period, members []string, filtered []model.FilteredCandidate, now time.Time) (*model.Digest, error) {
	if s.summarizer == nil {
		return s.fallbackDigest(roomID, period, filtered, digestFallbackReasonLLMDisabled, nil), nil
	}

	digest, err := s.summarizer.Summarize(ctx, &model.SummarizeInput{
		Period:      period,
		Now:         now,
		RoomID:      roomID,
		RoomMembers: members,
		Candidates:  filtered,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("summarize member news: %w", errors.Join(err, ctxErr))
		}

		return s.fallbackDigest(roomID, period, filtered, digestFallbackReasonFor(err), err), nil
	}

	if digest == nil || len(digest.TopItems) == 0 {
		return s.fallbackDigest(roomID, period, filtered, digestFallbackReasonEmptyResult, nil), nil
	}

	observeDigestResult(digest.ResultType, digestFallbackReasonNone)

	return digest, nil
}

func digestFallbackReasonFor(err error) string {
	switch {
	case errors.Is(err, newssummarizer.ErrLLMUnavailable):
		return digestFallbackReasonLLMDisabled
	case errors.Is(err, newssummarizer.ErrNoValidatedItems):
		return digestFallbackReasonValidationEmpty
	default:
		return digestFallbackReasonSummarizerError
	}
}

func (s *Service) fallbackDigest(roomID string, period model.Period, filtered []model.FilteredCandidate, reason string, cause error) *model.Digest {
	digest := newssummarizer.BuildDeterministicFallback(period, filtered)

	digest.TotalCount = len(filtered)
	observeDigestResult(digest.ResultType, reason)

	attrs := []any{
		slog.String("room_id", roomID),
		slog.String("period", string(period)),
		slog.String("reason", reason),
	}

	if cause != nil {
		attrs = append(attrs, slog.String("error", cause.Error()))
	}

	s.logger.Warn("Member news digest uses deterministic fallback", attrs...)

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
		return errors.New("membernews repository is nil")
	}

	if err := s.repository.Subscribe(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("subscribe room: %w", err)
	}

	return nil
}

func (s *Service) UnsubscribeRoom(ctx context.Context, roomID string) error {
	if s == nil || s.repository == nil {
		return errors.New("membernews repository is nil")
	}

	if err := s.repository.Unsubscribe(ctx, roomID); err != nil {
		return fmt.Errorf("unsubscribe room: %w", err)
	}

	return nil
}

func (s *Service) IsRoomSubscribed(ctx context.Context, roomID string) (bool, error) {
	if s == nil || s.repository == nil {
		return false, errors.New("membernews repository is nil")
	}

	subscribed, err := s.repository.IsSubscribed(ctx, roomID)
	if err != nil {
		return false, fmt.Errorf("check room subscription: %w", err)
	}

	return subscribed, nil
}

func (s *Service) ListSubscribedRooms(ctx context.Context) ([]model.SubscribedRoom, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("membernews repository is nil")
	}

	rooms, err := s.repository.ListSubscribedRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscribed rooms: %w", err)
	}

	return rooms, nil
}

package membernews

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// Service: 룸별 구독 멤버 뉴스를 생성합니다.
type Service struct {
	repository      *Repository
	summarizer      model.Summarizer
	sourceValidator *SourceValidator
	membersData     domain.MemberDataProvider
	logger          *slog.Logger
	now             func() time.Time
}

// NewService: membernews 서비스 생성.
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

// SetClock: 테스트를 위한 시간 주입.
func (s *Service) SetClock(clockFn func() time.Time) {
	if s == nil || clockFn == nil {
		return
	}
	s.now = clockFn
}

// GenerateRoomDigest: room 기준 뉴스 다이제스트를 생성합니다.
func (s *Service) GenerateRoomDigest(ctx context.Context, roomID string, period model.Period) (*model.Digest, error) {
	if s == nil {
		return nil, fmt.Errorf("membernews service is nil")
	}
	if s.repository == nil {
		return nil, fmt.Errorf("membernews repository is nil")
	}
	if stringutil.TrimSpace(roomID) == "" {
		return nil, fmt.Errorf("room id is required")
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
		return &model.Digest{
			ResultType:   sharedmodel.SummaryResultEmpty,
			Period:       normalizedPeriod,
			Headline:     model.DefaultHeadline(normalizedPeriod),
			TopItems:     []model.SummaryItem{},
			MoreSummary:  "",
			OmittedCount: 0,
			TotalCount:   0,
		}, nil
	}

	if s.summarizer == nil {
		digest := BuildDeterministicFallback(normalizedPeriod, filtered)
		digest.TotalCount = len(filtered)
		return digest, nil
	}

	digest, err := s.summarizer.Summarize(ctx, model.SummarizeInput{
		Period:      normalizedPeriod,
		Now:         s.now(),
		RoomID:      roomID,
		RoomMembers: members,
		Candidates:  filtered,
	})
	if err != nil {
		s.logger.Warn("Member news summarize failed, using deterministic fallback",
			slog.String("room_id", roomID),
			slog.String("period", string(normalizedPeriod)),
			slog.String("error", err.Error()),
		)
		digest = BuildDeterministicFallback(normalizedPeriod, filtered)
	}

	if digest == nil || len(digest.TopItems) == 0 {
		digest = BuildDeterministicFallback(normalizedPeriod, filtered)
	}

	digest.Period = normalizedPeriod
	if digest.Headline == "" {
		digest.Headline = model.DefaultHeadline(normalizedPeriod)
	}
	if digest.OmittedCount < 0 {
		digest.OmittedCount = 0
	}
	digest.TotalCount = len(filtered)

	return digest, nil
}

// SubscribeRoom: 방 뉴스 구독을 활성화합니다.
func (s *Service) SubscribeRoom(ctx context.Context, roomID, roomName string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.Subscribe(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("subscribe room: %w", err)
	}
	return nil
}

// UnsubscribeRoom: 방 뉴스 구독을 비활성화합니다.
func (s *Service) UnsubscribeRoom(ctx context.Context, roomID string) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.Unsubscribe(ctx, roomID); err != nil {
		return fmt.Errorf("unsubscribe room: %w", err)
	}
	return nil
}

// IsRoomSubscribed: 방 구독 상태를 반환합니다.
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

// ListSubscribedRooms: 구독 방 목록을 조회합니다.
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

// WarmupSubscriptionCache: DB 기준으로 구독 캐시를 재적재합니다.
func (s *Service) WarmupSubscriptionCache(ctx context.Context) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("membernews repository is nil")
	}
	if err := s.repository.WarmupCacheFromDB(ctx); err != nil {
		return fmt.Errorf("warmup subscription cache: %w", err)
	}
	return nil
}

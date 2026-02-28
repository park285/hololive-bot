package membernews

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	// ErrNoSubscribedMembers: 방에 설정된 알람 멤버가 없을 때 반환합니다.
	ErrNoSubscribedMembers = errors.New("no subscribed members")
)

// Period: 뉴스 요약 기간 타입.
type Period string

const (
	PeriodWeekly  Period = "weekly"
	PeriodMonthly Period = "monthly"
)

// SourceTier: 출처 신뢰도 등급.
type SourceTier string

const (
	SourceTierOfficial  SourceTier = "official"
	SourceTierMedia     SourceTier = "media"
	SourceTierCommunity SourceTier = "community"
)

// Category: 뉴스 분류 카테고리.
type Category string

const (
	CategoryBirthdayLive Category = "birthday_live"
	CategorySoloLive     Category = "solo_live"
	CategoryCollab       Category = "collab"
	CategoryEvent        Category = "event"
	CategoryGoods        Category = "goods"
	CategoryOther        Category = "other"
)

// Candidate: major_events 기반 원본 후보.
type Candidate struct {
	ID             int
	Type           domain.MajorEventType
	Title          string
	Description    string
	Members        []string
	PubDate        *time.Time
	EventStartDate *time.Time
	SourceURL      string
}

// FilteredCandidate: 기간/멤버/정렬/소스 검증이 적용된 후보.
type FilteredCandidate struct {
	Candidate      Candidate
	EffectiveDate  time.Time
	MatchedMembers []string
	MemberText     string
	Category       Category
	SourceTier     SourceTier
	SourceURL      string
}

// SummaryItem: 최종 뉴스 항목.
type SummaryItem struct {
	Member    string `json:"member"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	DateText  string `json:"date_text"`
	Summary   string `json:"summary"`
	SourceURL string `json:"source_url"`
}

// Digest: 룸별 뉴스 결과.
type Digest struct {
	Period       Period        `json:"period"`
	Headline     string        `json:"headline"`
	TopItems     []SummaryItem `json:"top_items"`
	MoreSummary  string        `json:"more_summary"`
	OmittedCount int           `json:"omitted_count"`
	TotalCount   int           `json:"total_count"`
}

// SummarizeInput: 요약기에 전달하는 입력.
type SummarizeInput struct {
	Period      Period
	Now         time.Time
	RoomID      string
	RoomMembers []string
	Candidates  []FilteredCandidate
}

// Summarizer: LLM/규칙 기반 요약 인터페이스.
type Summarizer interface {
	Summarize(ctx context.Context, input SummarizeInput) (*Digest, error)
}

// Service: 룸별 구독 멤버 뉴스를 생성합니다.
type Service struct {
	repository      *Repository
	summarizer      Summarizer
	sourceValidator *SourceValidator
	membersData     domain.MemberDataProvider
	logger          *slog.Logger
	now             func() time.Time
}

// NewService: membernews 서비스 생성.
func NewService(
	repository *Repository,
	summarizer Summarizer,
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
func (s *Service) GenerateRoomDigest(ctx context.Context, roomID string, period Period) (*Digest, error) {
	if s == nil {
		return nil, fmt.Errorf("membernews service is nil")
	}
	if s.repository == nil {
		return nil, fmt.Errorf("membernews repository is nil")
	}
	if stringutil.TrimSpace(roomID) == "" {
		return nil, fmt.Errorf("room id is required")
	}

	normalizedPeriod := NormalizePeriod(period)
	members, err := s.repository.GetRoomMembers(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room members: %w", err)
	}
	if len(members) == 0 {
		return nil, ErrNoSubscribedMembers
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
		return &Digest{
			Period:       normalizedPeriod,
			Headline:     defaultHeadline(normalizedPeriod),
			TopItems:     []SummaryItem{},
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

	digest, err := s.summarizer.Summarize(ctx, SummarizeInput{
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
		digest.Headline = defaultHeadline(normalizedPeriod)
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
func (s *Service) ListSubscribedRooms(ctx context.Context) ([]SubscribedRoom, error) {
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

// NormalizePeriod: 입력 기간 문자열을 canonical period로 정규화합니다.
func NormalizePeriod(period Period) Period {
	normalized := stringutil.Normalize(string(period))
	switch normalized {
	case "monthly", "이번달", "월간":
		return PeriodMonthly
	default:
		return PeriodWeekly
	}
}

func defaultHeadline(period Period) string {
	switch NormalizePeriod(period) {
	case PeriodMonthly:
		return "📅 이번달 구독 멤버 뉴스"
	default:
		return "🗞️ 이번주 구독 멤버 뉴스"
	}
}

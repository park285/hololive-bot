package model

import (
	"context"
	"errors"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	// ErrNoSubscribedMembers: 방에 설정된 알람 멤버가 없을 때 반환합니다.
	ErrNoSubscribedMembers = errors.New("no subscribed members")
)

// KST: 한국 표준시 (UTC+9).
var KST = time.FixedZone("KST", 9*60*60)

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

// SourceURLValidator: 출처 URL 검증기 인터페이스.
type SourceURLValidator interface {
	ValidateSourceURL(rawURL string) (SourceTier, string, error)
	HasCorroboration(text string) bool
}

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
	ResultType   sharedmodel.SummaryResultType `json:"result_type,omitempty"`
	Period       Period                        `json:"period"`
	Headline     string                        `json:"headline"`
	TopItems     []SummaryItem                 `json:"top_items"`
	MoreSummary  string                        `json:"more_summary"`
	OmittedCount int                           `json:"omitted_count"`
	TotalCount   int                           `json:"total_count"`
}

// SummarizeInput: 요약기에 전달하는 입력.
type SummarizeInput struct {
	Period      Period
	Now         time.Time
	RoomID      string
	RoomMembers []string
	Candidates  []FilteredCandidate
}

// SubscribedRoom: 뉴스 알림 구독 방 정보.
type SubscribedRoom struct {
	ID        int
	RoomID    string
	RoomName  string
	CreatedAt time.Time
}

// Summarizer: LLM/규칙 기반 요약 인터페이스.
type Summarizer interface {
	Summarize(ctx context.Context, input SummarizeInput) (*Digest, error)
}

// DigestFormatter: 다이제스트 메시지 formatter 인터페이스.
type DigestFormatter interface {
	FormatMemberNewsDigest(ctx context.Context, digest *Digest) string
}

// DigestService: scheduler가 Service에 기대는 최소 인터페이스.
type DigestService interface {
	GenerateRoomDigest(ctx context.Context, roomID string, period Period) (*Digest, error)
	ListSubscribedRooms(ctx context.Context) ([]SubscribedRoom, error)
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

// DefaultHeadline: 기간별 기본 헤드라인 문자열.
func DefaultHeadline(period Period) string {
	switch NormalizePeriod(period) {
	case PeriodMonthly:
		return "📅 이번달 구독 멤버 뉴스"
	default:
		return "🗞️ 이번주 구독 멤버 뉴스"
	}
}

package membernews

import (
	"errors"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
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

// SubscribedRoom: 뉴스 알림 구독 방 정보.
type SubscribedRoom struct {
	ID        int       `json:"id"`
	RoomID    string    `json:"room_id"`
	RoomName  string    `json:"room_name"`
	CreatedAt time.Time `json:"created_at"`
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

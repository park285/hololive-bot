// Package newsmodel은 member news 서비스·필터·요약기·스케줄러가 함께 쓰는 llm plane 내부 타입을 소유한다.
// bot plane과 주고받는 값 타입(Digest, SummaryItem, Period)은 in-process 계약 패키지 planes/llm/membernews/model에 있다.
package newsmodel

import (
	"context"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type SourceTier string

const (
	SourceTierOfficial  SourceTier = "official"
	SourceTierMedia     SourceTier = "media"
	SourceTierCommunity SourceTier = "community"
)

type SourceURLValidator interface {
	ValidateSourceURL(rawURL string) (SourceTier, string, error)
	HasCorroboration(text string) bool
}

type Category string

const (
	CategoryBirthdayLive Category = "birthday_live"
	CategorySoloLive     Category = "solo_live"
	CategoryCollab       Category = "collab"
	CategoryEvent        Category = "event"
	CategoryGoods        Category = "goods"
	CategoryOther        Category = "other"
)

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

type FilteredCandidate struct {
	Candidate      Candidate
	EffectiveDate  time.Time
	MatchedMembers []string
	MemberText     string
	Category       Category
	SourceTier     SourceTier
	SourceURL      string
}

type SummarizeInput struct {
	Period      model.Period
	Now         time.Time
	RoomID      string
	RoomMembers []string
	Candidates  []FilteredCandidate
}

type SubscribedRoom struct {
	ID        int
	RoomID    string
	RoomName  string
	CreatedAt time.Time
}

type Summarizer interface {
	Summarize(ctx context.Context, input *SummarizeInput) (*model.Digest, error)
}

// DigestFormatter의 렌더 실패는 오류다. 호출자는 실패한 방을 enqueue하지 않고 실패로 센다.
type DigestFormatter interface {
	FormatMemberNewsDigest(ctx context.Context, digest *model.Digest) (delivery.MessageVariants, error)
}

type DigestGenerator interface {
	GenerateRoomDigest(ctx context.Context, roomID string, period model.Period) (*model.Digest, error)
}

type DigestService interface {
	DigestGenerator
	ListSubscribedRooms(ctx context.Context) ([]SubscribedRoom, error)
	PrepareDigestRun(ctx context.Context, period model.Period, now time.Time) (DigestGenerator, error)
}

func DefaultHeadline(period model.Period) string {
	switch model.NormalizePeriod(period) {
	case model.PeriodMonthly:
		return "📅 이번달 구독 멤버 뉴스"
	case model.PeriodWeekly:
		return "🗞️ 이번주 구독 멤버 뉴스"
	default:
		return "🗞️ 이번주 구독 멤버 뉴스"
	}
}

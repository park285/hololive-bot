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

package model

import (
	"context"
	"errors"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var (
	ErrNoSubscribedMembers = errors.New("no subscribed members")
)

var KST = time.FixedZone("KST", 9*60*60)

type Period string

const (
	PeriodWeekly  Period = "weekly"
	PeriodMonthly Period = "monthly"
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

type SummaryItem struct {
	Member    string `json:"member"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	DateText  string `json:"date_text"`
	Summary   string `json:"summary"`
	SourceURL string `json:"source_url"`
}

type Digest struct {
	ResultType   sharedmodel.SummaryResultType `json:"result_type,omitempty"`
	Period       Period                        `json:"period"`
	Headline     string                        `json:"headline"`
	TopItems     []SummaryItem                 `json:"top_items"`
	MoreSummary  string                        `json:"more_summary"`
	OmittedCount int                           `json:"omitted_count"`
	TotalCount   int                           `json:"total_count"`
}

type SummarizeInput struct {
	Period      Period
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
	Summarize(ctx context.Context, input SummarizeInput) (*Digest, error)
}

type DigestFormatter interface {
	FormatMemberNewsDigest(ctx context.Context, digest *Digest) string
}

type DigestService interface {
	GenerateRoomDigest(ctx context.Context, roomID string, period Period) (*Digest, error)
	ListSubscribedRooms(ctx context.Context) ([]SubscribedRoom, error)
}

func NormalizePeriod(period Period) Period {
	normalized := stringutil.Normalize(string(period))
	switch normalized {
	case "monthly", "이번달", "월간":
		return PeriodMonthly
	default:
		return PeriodWeekly
	}
}

func DefaultHeadline(period Period) string {
	switch NormalizePeriod(period) {
	case PeriodMonthly:
		return "📅 이번달 구독 멤버 뉴스"
	default:
		return "🗞️ 이번주 구독 멤버 뉴스"
	}
}

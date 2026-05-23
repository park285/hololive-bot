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
	"errors"
	"time"

	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

var (
	// ErrNoSubscribedMembers: 방에 설정된 알람 멤버가 없을 때 반환합니다.
	ErrNoSubscribedMembers = errors.New("no subscribed members")
)

type Period string

const (
	PeriodWeekly  Period = "weekly"
	PeriodMonthly Period = "monthly"
)

type SummaryItem struct {
	Member    string `json:"member"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	DateText  string `json:"date_text"`
	Summary   string `json:"summary"`
	SourceURL string `json:"source_url"`
}

type Digest struct {
	Period       Period        `json:"period"`
	Headline     string        `json:"headline"`
	TopItems     []SummaryItem `json:"top_items"`
	MoreSummary  string        `json:"more_summary"`
	OmittedCount int           `json:"omitted_count"`
	TotalCount   int           `json:"total_count"`
}

type SubscribedRoom struct {
	ID        int       `json:"id"`
	RoomID    string    `json:"room_id"`
	RoomName  string    `json:"room_name"`
	CreatedAt time.Time `json:"created_at"`
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

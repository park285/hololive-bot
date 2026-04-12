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
	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

// NOTE: membernews는 외부 소비자(봇/스케줄러/어드민) 호환을 위해 root 패키지 API를 유지합니다.

var (
	ErrNoSubscribedMembers = model.ErrNoSubscribedMembers
)

type (
	Period            = model.Period
	SummaryResultType = sharedmodel.SummaryResultType
	SourceTier        = model.SourceTier
	Category          = model.Category
	Candidate         = model.Candidate
	FilteredCandidate = model.FilteredCandidate
	SummaryItem       = model.SummaryItem
	Digest            = model.Digest
	SummarizeInput    = model.SummarizeInput
	Summarizer        = model.Summarizer
	DigestFormatter   = model.DigestFormatter
	SubscribedRoom    = model.SubscribedRoom
)

const (
	SummaryResultPrimary  = sharedmodel.SummaryResultPrimary
	SummaryResultFallback = sharedmodel.SummaryResultFallback
	SummaryResultEmpty    = sharedmodel.SummaryResultEmpty
)

const (
	PeriodWeekly  = model.PeriodWeekly
	PeriodMonthly = model.PeriodMonthly
)

const (
	SourceTierOfficial  = model.SourceTierOfficial
	SourceTierMedia     = model.SourceTierMedia
	SourceTierCommunity = model.SourceTierCommunity
)

const (
	CategoryBirthdayLive = model.CategoryBirthdayLive
	CategorySoloLive     = model.CategorySoloLive
	CategoryCollab       = model.CategoryCollab
	CategoryEvent        = model.CategoryEvent
	CategoryGoods        = model.CategoryGoods
	CategoryOther        = model.CategoryOther
)

func NormalizePeriod(period Period) Period {
	return model.NormalizePeriod(period)
}

func defaultHeadline(period Period) string {
	return model.DefaultHeadline(period)
}

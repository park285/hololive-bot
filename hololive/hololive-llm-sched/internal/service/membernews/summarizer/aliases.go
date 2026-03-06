package summarizer

import (
	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
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

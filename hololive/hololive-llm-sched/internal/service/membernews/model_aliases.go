package membernews

import "github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"

// NOTE: membernews는 외부 소비자(봇/스케줄러/어드민) 호환을 위해 root 패키지 API를 유지합니다.

var (
	ErrNoSubscribedMembers = model.ErrNoSubscribedMembers
)

type (
	Period            = model.Period
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

// NormalizePeriod: 입력 period를 canonical 값으로 정규화합니다.
func NormalizePeriod(period Period) Period {
	return model.NormalizePeriod(period)
}

func defaultHeadline(period Period) string {
	return model.DefaultHeadline(period)
}

package membernews

import (
	"log/slog"

	sharedmodel "github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/summarizer"
)

type (
	LLMClient           = summarizer.LLMClient
	SearchResult        = sharedmodel.SearchResult
	WebSearcher         = sharedmodel.WebSearcher
	SummarizerImpl      = summarizer.SummarizerImpl
	ConsensusSummarizer = summarizer.ConsensusSummarizer
)

// NewSummarizer: 요약기 생성. (호환 wrapper)
func NewSummarizer(
	llm LLMClient,
	searcher WebSearcher,
	validator *SourceValidator,
	logger *slog.Logger,
) *SummarizerImpl {
	var v model.SourceURLValidator
	if validator != nil {
		v = validator
	}
	return summarizer.NewSummarizer(llm, searcher, v, logger)
}

// NewConsensusSummarizer: consensus 요약기 생성. (호환 wrapper)
func NewConsensusSummarizer(
	primary Summarizer,
	reviewer LLMClient,
	adjudicator LLMClient,
	validator *SourceValidator,
	cfg consensus.Config,
	logger *slog.Logger,
) *ConsensusSummarizer {
	var v model.SourceURLValidator
	if validator != nil {
		v = validator
	}
	return summarizer.NewConsensusSummarizer(primary, reviewer, adjudicator, v, cfg, logger)
}

// BuildDeterministicFallback: LLM 실패/검증 실패 시 고정 규칙 출력 생성.
func BuildDeterministicFallback(period Period, candidates []FilteredCandidate) *Digest {
	return summarizer.BuildDeterministicFallback(period, candidates)
}

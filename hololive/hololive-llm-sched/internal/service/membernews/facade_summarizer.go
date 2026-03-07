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

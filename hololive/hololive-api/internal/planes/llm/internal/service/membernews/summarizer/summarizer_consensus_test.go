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

package summarizer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

// fakeLLMWithCounter: 호출 횟수를 추적하는 LLM 모의 클라이언트.
type fakeLLMWithCounter struct {
	response  string
	err       error
	callCount atomic.Int32
}

func (f *fakeLLMWithCounter) GenerateJSON(_ context.Context, _, _ string, _ map[string]any) (string, error) {
	f.callCount.Add(1)
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

// fakeSummarizer: primary Summarizer 모의 구현.
type fakeSummarizer struct {
	digest *model.Digest
	err    error
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ *model.SummarizeInput) (*model.Digest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.digest, nil
}

func defaultConsensusConfig() consensus.Config {
	return consensus.Config{
		ConfidenceThreshold: 0.85,
		ReviewTimeout:       5 * time.Second,
		AdjudicateTimeout:   5 * time.Second,
	}
}

func defaultTestInput() *model.SummarizeInput {
	return &model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"사쿠라 미코"},
		Candidates:  sampleCandidates(),
	}
}

func primaryDigest() *model.Digest {
	return &model.Digest{
		Period:   model.PeriodWeekly,
		Headline: "테스트 헤드라인",
		TopItems: []model.SummaryItem{
			{
				Member:    "사쿠라 미코",
				Category:  "event",
				Title:     "EXPO",
				DateText:  "2026-02-20",
				Summary:   "요약",
				SourceURL: "https://hololive.hololivepro.com/news/1",
			},
		},
		MoreSummary:  "",
		OmittedCount: 0,
		TotalCount:   1,
	}
}

func approvedVerdictJSON(confidence float64) string {
	v := consensus.ReviewVerdict{
		Approved:   true,
		Issues:     []consensus.ReviewIssue{},
		Confidence: confidence,
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func criticalVerdictJSON() string {
	v := consensus.ReviewVerdict{
		Approved: false,
		Issues: []consensus.ReviewIssue{
			{Field: "source_url", ItemIndex: 0, Severity: "critical", Description: "URL fabricated"},
		},
		Confidence: 0.3,
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func lowConfidenceVerdictJSON() string {
	v := consensus.ReviewVerdict{
		Approved:   false,
		Issues:     []consensus.ReviewIssue{},
		Confidence: 0.5,
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func warningOnlyVerdictJSON() string {
	v := consensus.ReviewVerdict{
		Approved: true,
		Issues: []consensus.ReviewIssue{
			{Field: "category", ItemIndex: 0, Severity: "warning", Description: "minor mismatch"},
		},
		Confidence: 0.9,
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func adjudicatorResponseJSON(title string) string {
	r := summaryResponse{
		Period:   "weekly",
		Headline: "수정된 헤드라인",
		TopItems: []summaryResponseItem{
			{Member: "사쿠라 미코", Category: "event", Title: title, DateText: "2026-02-20", Summary: "수정된 요약", SourceURL: "https://hololive.hololivepro.com/news/1"},
		},
		MoreSummary:  "",
		OmittedCount: 0,
	}
	b, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestConsensus_ReviewerApproves_PassThrough(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: approvedVerdictJSON(0.95)}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
	if reviewer.callCount.Load() != 1 {
		t.Errorf("reviewer should be called once, got %d", reviewer.callCount.Load())
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_CriticalIssues_TriggersAdjudication(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{response: adjudicatorResponseJSON("수정된 EXPO")}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator,
		mustValidatorWithAllowlist(t),
		defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adjudicator.callCount.Load() != 1 {
		t.Fatalf("adjudicator should be called once, got %d", adjudicator.callCount.Load())
	}
	if len(digest.TopItems) == 0 {
		t.Fatal("expected adjudicator items")
	}
	if digest.TopItems[0].Title != "수정된 EXPO" {
		t.Errorf("expected adjudicator title, got %q", digest.TopItems[0].Title)
	}
}

func TestConsensus_ReviewerFails_GracefulDegradation(t *testing.T) {
	reviewer := &fakeLLMWithCounter{err: errors.New("reviewer timeout")}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
	if reviewer.callCount.Load() != 1 {
		t.Errorf("reviewer should be called once, got %d", reviewer.callCount.Load())
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called on reviewer failure, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_AdjudicatorFails_ReturnsPrimary(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{err: errors.New("adjudicator down")}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
	if adjudicator.callCount.Load() != 1 {
		t.Errorf("adjudicator should be called once, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_AdjudicatorNil_SkipsStage3(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, nil, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
}

func TestConsensus_LowConfidence_TriggersAdjudication(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: lowConfidenceVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{response: adjudicatorResponseJSON("수정됨")}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator,
		mustValidatorWithAllowlist(t),
		defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adjudicator.callCount.Load() != 1 {
		t.Fatalf("adjudicator should be called for low confidence, got %d", adjudicator.callCount.Load())
	}
	if digest.TopItems[0].Title != "수정됨" {
		t.Errorf("expected adjudicator title, got %q", digest.TopItems[0].Title)
	}
}

func TestConsensus_OnlyWarnings_NoAdjudication(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: warningOnlyVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called for warnings only, got %d", adjudicator.callCount.Load())
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
}

func TestConsensus_ValidationRunsOnAdjudicatorOutput(t *testing.T) {
	// adjudicator가 잘못된 URL이 포함된 2개 항목 반환
	badResponse := summaryResponse{
		Period:   "weekly",
		Headline: "수정됨",
		TopItems: []summaryResponseItem{
			{Member: "사쿠라 미코", Category: "event", Title: "Good", DateText: "2026-02-20", Summary: "요약", SourceURL: "https://hololive.hololivepro.com/news/1"},
			{Member: "B", Category: "event", Title: "Bad URL", DateText: "2026-02-20", Summary: "요약", SourceURL: "https://evil.com/fake"},
		},
		MoreSummary:  "",
		OmittedCount: 0,
	}
	badJSON, err := json.Marshal(badResponse)
	if err != nil {
		t.Fatalf("marshal bad response: %v", err)
	}

	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{response: string(badJSON)}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator,
		mustValidatorWithAllowlist(t),
		defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// validator가 bad URL 항목을 제거해야 함
	if len(digest.TopItems) >= 2 {
		t.Errorf("expected validator to drop bad URL item, got %d items", len(digest.TopItems))
	}
}

func TestConsensus_PrimaryEmpty_ReturnsFallback(t *testing.T) {
	// primary가 빈 digest 반환 (fallback 포함)
	emptyDigest := &model.Digest{
		Period:   model.PeriodWeekly,
		Headline: "🗞️ 이번주 구독 멤버 뉴스",
		TopItems: []model.SummaryItem{},
	}
	reviewer := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: emptyDigest},
		reviewer, nil, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(digest.TopItems) != 0 {
		t.Errorf("expected empty items from primary empty, got %d", len(digest.TopItems))
	}
	if reviewer.callCount.Load() != 0 {
		t.Errorf("reviewer should not be called when primary is empty, got %d", reviewer.callCount.Load())
	}
}

func TestConsensus_ReviewerMalformedJSON(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: `{invalid json`}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline on malformed JSON, got %q", digest.Headline)
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called on parse failure, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_ReviewerTimeout(t *testing.T) {
	// context deadline exceeded 시뮬레이션
	reviewer := &fakeLLMWithCounter{err: fmt.Errorf("reviewer: %w", context.DeadlineExceeded)}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline on timeout, got %q", digest.Headline)
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called on timeout, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_UnknownSeverity_TreatedAsInfo(t *testing.T) {
	// unknown severity → info로 정규화 → adjudication 미트리거
	v := consensus.ReviewVerdict{
		Approved: true,
		Issues: []consensus.ReviewIssue{
			{Field: "category", ItemIndex: 0, Severity: "unknown", Description: "test"},
			{Field: "title", ItemIndex: 0, Severity: "", Description: "empty"},
		},
		Confidence: 0.9,
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}

	reviewer := &fakeLLMWithCounter{response: string(b)}
	adjudicator := &fakeLLMWithCounter{}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adjudicator.callCount.Load() != 0 {
		t.Errorf("adjudicator should not be called for unknown severity, got %d", adjudicator.callCount.Load())
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline, got %q", digest.Headline)
	}
}

func TestConsensus_AdjudicatorMalformedJSON_ReturnsPrimary(t *testing.T) {
	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{response: `{not valid json`}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline on adjudicator parse fail, got %q", digest.Headline)
	}
	if adjudicator.callCount.Load() != 1 {
		t.Errorf("adjudicator should be called once, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_ValidationDropsAllAdjudicatorItems_ReturnsPrimary(t *testing.T) {
	// adjudicator가 모두 잘못된 URL인 항목만 반환
	allBadResponse := summaryResponse{
		Period:   "weekly",
		Headline: "수정됨",
		TopItems: []summaryResponseItem{
			{Member: "A", Category: "event", Title: "Bad1", DateText: "2026-02-20", Summary: "요약", SourceURL: "https://evil.com/fake1"},
			{Member: "B", Category: "event", Title: "Bad2", DateText: "2026-02-20", Summary: "요약", SourceURL: "https://evil.com/fake2"},
		},
		MoreSummary:  "",
		OmittedCount: 0,
	}
	allBadJSON, err := json.Marshal(allBadResponse)
	if err != nil {
		t.Fatalf("marshal all bad response: %v", err)
	}

	reviewer := &fakeLLMWithCounter{response: criticalVerdictJSON()}
	adjudicator := &fakeLLMWithCounter{response: string(allBadJSON)}

	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		reviewer, adjudicator,
		mustValidatorWithAllowlist(t),
		defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// validation이 모든 항목을 제거 → primary로 fallback
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline when all adjudicator items dropped, got %q", digest.Headline)
	}
	if adjudicator.callCount.Load() != 1 {
		t.Errorf("adjudicator should be called once, got %d", adjudicator.callCount.Load())
	}
}

func TestConsensus_ReviewerNil_ReturnsPrimary(t *testing.T) {
	cs := NewConsensusSummarizer(
		&fakeSummarizer{digest: primaryDigest()},
		nil, nil, nil, defaultConsensusConfig(), nil,
	)

	digest, err := cs.Summarize(context.Background(), defaultTestInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if digest.Headline != "테스트 헤드라인" {
		t.Errorf("expected primary headline when reviewer is nil, got %q", digest.Headline)
	}
}

func TestNormalizeSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"critical", "critical"},
		{"CRITICAL", "critical"},
		{"warning", "warning"},
		{"info", "info"},
		{"unknown", "info"},
		{"", "info"},
		{"  Warning  ", "warning"},
		{"error", "info"},
	}
	for _, tt := range tests {
		got := consensus.NormalizeSeverity(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeSeverity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

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
	"net/url"
	"strings"
	"testing"
	"time"

	sharedmodel "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

type fakeLLM struct {
	response string
	err      error
}

func (f *fakeLLM) GenerateJSON(_ context.Context, _, _ string, _ map[string]any) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

type testSourceValidator struct {
	allowedXAccounts map[string]struct{}
}

func (v *testSourceValidator) ValidateSourceURL(rawURL string) (model.SourceTier, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return model.SourceTierCommunity, "", fmt.Errorf("source url is empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return model.SourceTierCommunity, "", fmt.Errorf("parse source url: %w", err)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	host = strings.TrimPrefix(host, "www.")

	if host == "x.com" || host == "twitter.com" {
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(segments) == 0 || strings.TrimSpace(segments[0]) == "" {
			return model.SourceTierCommunity, "", fmt.Errorf("x.com account not found")
		}
		account := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(segments[0])), "@")
		if _, ok := v.allowedXAccounts[account]; !ok {
			return model.SourceTierCommunity, "", fmt.Errorf("x.com account not in allowlist: %s", account)
		}
		return model.SourceTierOfficial, parsed.String(), nil
	}

	switch host {
	case "hololive.hololivepro.com", "hololivepro.com", "cover-corp.com":
		return model.SourceTierOfficial, parsed.String(), nil
	case "prtimes.jp", "oricon.co.jp", "natalie.mu", "famitsu.com", "4gamer.net", "animate.tv", "dengekionline.com":
		return model.SourceTierMedia, parsed.String(), nil
	default:
		return model.SourceTierCommunity, parsed.String(), nil
	}
}

func (v *testSourceValidator) HasCorroboration(text string) bool {
	// 테스트에서는 "공식/미디어 링크가 하나라도 포함되면 corroboration"으로 간주.
	for _, needle := range []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"cover-corp.com",
		"prtimes.jp",
		"oricon.co.jp",
		"natalie.mu",
		"famitsu.com",
		"4gamer.net",
		"animate.tv",
		"dengekionline.com",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func TestSummarizer_SchemaSuccess(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(&fakeLLM{response: `{
  "period":"weekly",
  "headline":"테스트 헤드라인",
  "top_items":[
    {"member":"사쿠라 미코","category":"event","title":"EXPO","date_text":"2026-02-20","summary":"요약","source_url":"https://hololive.hololivepro.com/news/1"}
  ],
  "more_summary":"",
  "omitted_count":0
}`}, nil, validator, nil)

	input := model.SummarizeInput{Period: model.PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), &input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if len(digest.TopItems) != 1 {
		t.Fatalf("expected 1 top item, got %d", len(digest.TopItems))
	}
	if digest.ResultType != sharedmodel.SummaryResultPrimary {
		t.Fatalf("result_type = %q, want %q", digest.ResultType, sharedmodel.SummaryResultPrimary)
	}
	if digest.TopItems[0].SourceURL == "" {
		t.Fatalf("expected non-empty source url")
	}
}

func TestSummarizer_DropsInvalidItemsByValidator(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(&fakeLLM{response: `{
  "period":"weekly",
  "headline":"테스트",
  "top_items":[
    {"member":"A","category":"event","title":"no source","date_text":"2026-02-20","summary":"x","source_url":""},
    {"member":"B","category":"event","title":"bad x","date_text":"2026-02-20","summary":"x","source_url":"https://x.com/not_allowed/status/1"},
    {"member":"C","category":"event","title":"valid","date_text":"2026-02-20","summary":"x","source_url":"https://hololive.hololivepro.com/news/2"}
  ],
  "more_summary":"",
  "omitted_count":0
}`}, nil, validator, nil)

	input := model.SummarizeInput{Period: model.PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), &input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if len(digest.TopItems) != 1 {
		t.Fatalf("expected 1 valid top item after drop, got %d", len(digest.TopItems))
	}
	if digest.TopItems[0].Title != "valid" {
		t.Fatalf("expected remaining item to be valid, got %q", digest.TopItems[0].Title)
	}
}

func TestSummarizer_LLMFailureUsesFallback(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(&fakeLLM{err: errors.New("llm down")}, nil, validator, nil)

	input := model.SummarizeInput{Period: model.PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), &input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if len(digest.TopItems) == 0 {
		t.Fatalf("fallback should provide non-empty top items")
	}
	if digest.ResultType != sharedmodel.SummaryResultFallback {
		t.Fatalf("result_type = %q, want %q", digest.ResultType, sharedmodel.SummaryResultFallback)
	}
}

func TestSummarizer_OmittedCountUsesServerCalculatedValue(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(&fakeLLM{response: `{
  "period":"weekly",
  "headline":"테스트 헤드라인",
  "top_items":[
    {"member":"사쿠라 미코","category":"event","title":"EXPO","date_text":"2026-02-20","summary":"요약","source_url":"https://hololive.hololivepro.com/news/1"}
  ],
  "more_summary":"요약 문장",
  "omitted_count":99
}`}, nil, validator, nil)

	candidates := []model.FilteredCandidate{
		sampleCandidates()[0],
		{
			Candidate: model.Candidate{
				Title:       "SUISIEI LIVE",
				Description: "official event",
			},
			EffectiveDate: time.Date(2026, 2, 21, 12, 0, 0, 0, model.KST),
			MemberText:    "호시마치 스이세이",
			Category:      model.CategorySoloLive,
			SourceTier:    model.SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/2",
		},
		{
			Candidate: model.Candidate{
				Title:       "Miko Goods",
				Description: "official goods",
			},
			EffectiveDate: time.Date(2026, 2, 22, 12, 0, 0, 0, model.KST),
			MemberText:    "사쿠라 미코",
			Category:      model.CategoryGoods,
			SourceTier:    model.SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/3",
		},
	}

	input := model.SummarizeInput{
		Period:     model.PeriodWeekly,
		Now:        time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		Candidates: candidates,
	}
	digest, err := s.Summarize(context.Background(), &input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}

	if digest.TotalCount != 3 {
		t.Fatalf("expected total_count=3, got %d", digest.TotalCount)
	}
	if len(digest.TopItems) != 1 {
		t.Fatalf("expected top_items=1, got %d", len(digest.TopItems))
	}
	if digest.OmittedCount != 2 {
		t.Fatalf("expected omitted_count=2(total-top), got %d", digest.OmittedCount)
	}
}

func mustValidatorWithAllowlist(t *testing.T) model.SourceURLValidator {
	t.Helper()

	validator := &testSourceValidator{
		allowedXAccounts: map[string]struct{}{
			"hololivetv": {},
		},
	}
	return validator
}

func TestBuildDeterministicFallback_NaturalFormat(t *testing.T) {
	candidates := sampleCandidates()
	digest := BuildDeterministicFallback(model.PeriodWeekly, candidates)
	if digest.ResultType != sharedmodel.SummaryResultFallback {
		t.Fatalf("result_type = %q, want %q", digest.ResultType, sharedmodel.SummaryResultFallback)
	}
	for _, item := range digest.TopItems {
		if strings.Contains(item.Summary, "[") {
			t.Errorf("Summary should not contain brackets, got %q", item.Summary)
		}
		// M/D( 패턴 확인 (예: "2/20(")
		matched := false
		for i := 0; i < len(item.Summary); i++ {
			if item.Summary[i] == '/' && i > 0 && i+1 < len(item.Summary) {
				if item.Summary[i-1] >= '0' && item.Summary[i-1] <= '9' {
					matched = true
					break
				}
			}
		}
		if !matched {
			t.Errorf("Summary should contain M/D( date pattern, got %q", item.Summary)
		}
	}
}

func TestSummarizer_EmptyCandidatesUsesEmptyResultType(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(&fakeLLM{response: "{}"}, nil, validator, nil)

	digest, err := s.Summarize(context.Background(), &model.SummarizeInput{Period: model.PeriodWeekly})
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if digest.ResultType != sharedmodel.SummaryResultEmpty {
		t.Fatalf("result_type = %q, want %q", digest.ResultType, sharedmodel.SummaryResultEmpty)
	}
	if len(digest.TopItems) != 0 {
		t.Fatalf("expected empty top items, got %d", len(digest.TopItems))
	}
}

func TestBuildDeterministicFallback_CategoryLocalized(t *testing.T) {
	// sampleCandidates uses CategoryEvent → "이벤트"
	candidates := sampleCandidates()
	digest := BuildDeterministicFallback(model.PeriodWeekly, candidates)
	for _, item := range digest.TopItems {
		if strings.Contains(item.Summary, "event") || strings.Contains(item.Summary, "solo_live") {
			t.Errorf("Summary should use Korean category label, got %q", item.Summary)
		}
	}
}

func TestMemberNewsSystemPrompt_ContainsGuide(t *testing.T) {
	prompt := memberNewsSystemPrompt()
	for _, keyword := range []string{"translation_guide", "tone", "field_format"} {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("prompt should contain %q", keyword)
		}
	}
}

func sampleCandidates() []model.FilteredCandidate {
	date := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)
	return []model.FilteredCandidate{
		{
			Candidate: model.Candidate{
				Title:       "EXPO",
				Description: "official news",
			},
			EffectiveDate: date,
			MemberText:    "사쿠라 미코",
			Category:      model.CategoryEvent,
			SourceTier:    model.SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/1",
		},
	}
}

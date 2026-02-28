package membernews

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	input := SummarizeInput{Period: PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, kst), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if len(digest.TopItems) != 1 {
		t.Fatalf("expected 1 top item, got %d", len(digest.TopItems))
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

	input := SummarizeInput{Period: PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, kst), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), input)
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

	input := SummarizeInput{Period: PeriodWeekly, Now: time.Date(2026, 2, 16, 10, 0, 0, 0, kst), Candidates: sampleCandidates()}
	digest, err := s.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}
	if len(digest.TopItems) == 0 {
		t.Fatalf("fallback should provide non-empty top items")
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

	candidates := []FilteredCandidate{
		sampleCandidates()[0],
		{
			Candidate: Candidate{
				Title:       "SUISIEI LIVE",
				Description: "official event",
			},
			EffectiveDate: time.Date(2026, 2, 21, 12, 0, 0, 0, kst),
			MemberText:    "호시마치 스이세이",
			Category:      CategorySoloLive,
			SourceTier:    SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/2",
		},
		{
			Candidate: Candidate{
				Title:       "Miko Goods",
				Description: "official goods",
			},
			EffectiveDate: time.Date(2026, 2, 22, 12, 0, 0, 0, kst),
			MemberText:    "사쿠라 미코",
			Category:      CategoryGoods,
			SourceTier:    SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/3",
		},
	}

	input := SummarizeInput{
		Period:     PeriodWeekly,
		Now:        time.Date(2026, 2, 16, 10, 0, 0, 0, kst),
		Candidates: candidates,
	}
	digest, err := s.Summarize(context.Background(), input)
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

func mustValidatorWithAllowlist(t *testing.T) *SourceValidator {
	t.Helper()
	tempDir := t.TempDir()
	allowlistPath := filepath.Join(tempDir, "allowlist.json")
	if err := os.WriteFile(allowlistPath, []byte(`{"official_accounts":["hololivetv"]}`), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	validator, err := NewSourceValidator(allowlistPath, nil, nil)
	if err != nil {
		t.Fatalf("new source validator: %v", err)
	}
	return validator
}

func TestBuildDeterministicFallback_NaturalFormat(t *testing.T) {
	candidates := sampleCandidates()
	digest := BuildDeterministicFallback(PeriodWeekly, candidates)
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

func TestBuildDeterministicFallback_CategoryLocalized(t *testing.T) {
	// sampleCandidates uses CategoryEvent → "이벤트"
	candidates := sampleCandidates()
	digest := BuildDeterministicFallback(PeriodWeekly, candidates)
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

func sampleCandidates() []FilteredCandidate {
	date := time.Date(2026, 2, 20, 12, 0, 0, 0, kst)
	return []FilteredCandidate{
		{
			Candidate: Candidate{
				Title:       "EXPO",
				Description: "official news",
			},
			EffectiveDate: date,
			MemberText:    "사쿠라 미코",
			Category:      CategoryEvent,
			SourceTier:    SourceTierOfficial,
			SourceURL:     "https://hololive.hololivepro.com/news/1",
		},
	}
}

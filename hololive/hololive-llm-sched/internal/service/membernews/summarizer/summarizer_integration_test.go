package summarizer

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/llm"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// --- 테스트 헬퍼 ---

func skipIfNoLLMKey(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}
	_ = loadEnv()
	if os.Getenv("CLIPROXY_API_KEY") == "" {
		t.Skip("Skipping: CLIPROXY_API_KEY not set")
	}
}

func loadEnv() error {
	data, err := os.ReadFile("../../../../.env")
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
	return nil
}

func newTestMemberNewsClient(t *testing.T) LLMClient {
	t.Helper()
	model := os.Getenv("CLIPROXY_TEST_MODEL")
	if model == "" {
		model = "gpt-5.3-codex"
	}

	t.Logf("Model: %s, BaseURL: http://127.0.0.1:8787/v1 (Chat Completions)", model)

	return llm.NewClient(
		"http://127.0.0.1:8787/v1",
		os.Getenv("CLIPROXY_API_KEY"),
		model,
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		llm.WithSchemaName("member_news_summary"),
		llm.WithTemperature(0.3),
		llm.WithWebSearch(false),
		llm.WithChatCompletions(),
	)
}

func integrationCandidates() []FilteredCandidate {
	date := func(y, m, d int) time.Time {
		return time.Date(y, time.Month(m), d, 12, 0, 0, 0, kst)
	}
	return []FilteredCandidate{
		{
			Candidate: Candidate{
				Title:       "hololive SUPER EXPO 2026",
				Description: "마쿠하리 메세에서 개최되는 홀로라이브 최대 규모 행사",
			},
			EffectiveDate:  date(2026, 3, 6),
			MatchedMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
			MemberText:     "사쿠라 미코, 호시마치 스이세이",
			Category:       CategoryEvent,
			SourceTier:     SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/events/expo2026",
		},
		{
			Candidate: Candidate{
				Title:       "Hoshimachi Suisei Live SuperNova: REBOOT",
				Description: "호시마치 스이세이 솔로 라이브 콘서트",
			},
			EffectiveDate:  date(2026, 2, 20),
			MatchedMembers: []string{"호시마치 스이세이"},
			MemberText:     "호시마치 스이세이",
			Category:       CategorySoloLive,
			SourceTier:     SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/events/suisei2026",
		},
		{
			Candidate: Candidate{
				Title:       "사쿠라 미코 생일 기념 굿즈",
				Description: "사쿠라 미코 생일 기념 한정 굿즈 판매",
			},
			EffectiveDate:  date(2026, 3, 5),
			MatchedMembers: []string{"사쿠라 미코"},
			MemberText:     "사쿠라 미코",
			Category:       CategoryGoods,
			SourceTier:     SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/miko-birthday-goods",
		},
		{
			Candidate: Candidate{
				Title:       "hololive DEV_IS NEW WAVE POP UP STORE",
				Description: "DEV_IS 소속 멤버 팝업스토어 개최",
			},
			EffectiveDate:  date(2026, 2, 14),
			MatchedMembers: []string{"사쿠라 미코"},
			MemberText:     "사쿠라 미코",
			Category:       CategoryOther,
			SourceTier:     SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/devis-popup",
		},
		{
			Candidate: Candidate{
				Title:       "ホロライブ バレンタイン 2026",
				Description: "HMV&BOOKS SHIBUYA 팝업 이벤트",
			},
			EffectiveDate:  date(2026, 2, 7),
			MatchedMembers: []string{"호시마치 스이세이"},
			MemberText:     "호시마치 스이세이",
			Category:       CategoryEvent,
			SourceTier:     SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/valentine2026",
		},
	}
}

// === 통합 테스트 ===

func TestIntegration_MemberNewsSummarize_Weekly(t *testing.T) {
	skipIfNoLLMKey(t)

	client := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(client, nil, validator, nil)

	input := SummarizeInput{
		Period:      PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, kst),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}

	if len(digest.TopItems) == 0 {
		t.Fatal("top_items가 비어있음")
	}

	for i, item := range digest.TopItems {
		t.Logf("item[%d]: member=%q category=%q title=%q source=%q",
			i, item.Member, item.Category, item.Title, item.SourceURL)
		if strings.TrimSpace(item.SourceURL) == "" {
			t.Errorf("item[%d].source_url이 비어있음", i)
		}
	}

	// JSON 직렬화 검증
	raw, err := json.Marshal(digest)
	if err != nil {
		t.Fatalf("digest JSON 직렬화 실패: %v", err)
	}
	t.Logf("digest JSON: %s", string(raw))
}

func TestIntegration_MemberNewsSummarize_Monthly(t *testing.T) {
	skipIfNoLLMKey(t)

	client := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(client, nil, validator, nil)

	input := SummarizeInput{
		Period:      PeriodMonthly,
		Now:         time.Date(2026, 3, 1, 10, 0, 0, 0, kst),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}

	if len(digest.TopItems) == 0 {
		t.Fatal("top_items가 비어있음")
	}

	t.Logf("headline: %q", digest.Headline)
	t.Logf("period: %q", digest.Period)
	for i, item := range digest.TopItems {
		t.Logf("item[%d]: member=%q category=%q title=%q source=%q",
			i, item.Member, item.Category, item.Title, item.SourceURL)
	}

	// 월간 period 반환 검증
	if digest.Period != PeriodMonthly {
		t.Errorf("expected period %q, got %q", PeriodMonthly, digest.Period)
	}
}

func TestIntegration_MemberNewsSummarize_SchemaCompliance(t *testing.T) {
	skipIfNoLLMKey(t)

	client := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(client, nil, validator, nil)

	input := SummarizeInput{
		Period:      PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, kst),
		RoomMembers: []string{"사쿠라 미코"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}

	// period enum 검증
	if digest.Period != PeriodWeekly && digest.Period != PeriodMonthly {
		t.Errorf("unexpected period enum: %q", digest.Period)
	}

	// required fields 검증
	if strings.TrimSpace(digest.Headline) == "" {
		t.Error("headline이 비어있음")
	}

	if len(digest.TopItems) == 0 {
		t.Fatal("top_items가 비어있음 (validateAndBuildDigest 통과 실패)")
	}

	for i, item := range digest.TopItems {
		if strings.TrimSpace(item.Member) == "" {
			t.Errorf("item[%d].member가 비어있음", i)
		}
		if strings.TrimSpace(item.Category) == "" {
			t.Errorf("item[%d].category가 비어있음", i)
		}
		if strings.TrimSpace(item.Title) == "" {
			t.Errorf("item[%d].title이 비어있음", i)
		}
		if strings.TrimSpace(item.DateText) == "" {
			t.Errorf("item[%d].date_text가 비어있음", i)
		}
		if strings.TrimSpace(item.Summary) == "" {
			t.Errorf("item[%d].summary가 비어있음", i)
		}
		if strings.TrimSpace(item.SourceURL) == "" {
			t.Errorf("item[%d].source_url이 비어있음", i)
		}
	}

	// OmittedCount 정합성
	if digest.OmittedCount < 0 {
		t.Errorf("omitted_count가 음수: %d", digest.OmittedCount)
	}

	// TotalCount = 입력 후보 수
	if digest.TotalCount != len(input.Candidates) {
		t.Errorf("total_count %d != input candidates %d", digest.TotalCount, len(input.Candidates))
	}

	t.Logf("schema compliance OK: period=%q headline=%q items=%d omitted=%d total=%d",
		digest.Period, digest.Headline, len(digest.TopItems), digest.OmittedCount, digest.TotalCount)
}

func TestIntegration_Consensus_FullPipeline(t *testing.T) {
	skipIfNoLLMKey(t)

	primaryClient := newTestMemberNewsClient(t)
	reviewerClient := newTestMemberNewsClient(t)
	adjudicatorClient := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)

	baseSummarizer := NewSummarizer(primaryClient, nil, validator, nil)
	cs := NewConsensusSummarizer(
		baseSummarizer, reviewerClient, adjudicatorClient, validator,
		ConsensusConfig{
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		},
		nil,
	)

	input := SummarizeInput{
		Period:      PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, kst),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := cs.Summarize(context.Background(), input)
	if err != nil {
		t.Fatalf("consensus summarize error: %v", err)
	}

	if len(digest.TopItems) == 0 {
		t.Fatal("consensus pipeline returned empty top_items")
	}

	for i, item := range digest.TopItems {
		t.Logf("consensus item[%d]: member=%q category=%q title=%q source=%q",
			i, item.Member, item.Category, item.Title, item.SourceURL)
		if strings.TrimSpace(item.SourceURL) == "" {
			t.Errorf("item[%d].source_url is empty", i)
		}
	}

	t.Logf("consensus digest: period=%q headline=%q items=%d omitted=%d total=%d",
		digest.Period, digest.Headline, len(digest.TopItems), digest.OmittedCount, digest.TotalCount)
}

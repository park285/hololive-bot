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
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	json "github.com/park285/shared-go/pkg/json"
)

func skipIfNoLLMKey(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TEST=true to run)")
	}
	if err := loadEnv(); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("load env: %v", err)
	}
	if os.Getenv("CLIPROXY_API_KEY") == "" {
		t.Skip("Skipping: CLIPROXY_API_KEY not set")
	}
}

func loadEnv() error {
	candidates := []string{
		os.Getenv("HOLOLIVE_API_ENV_FILE"),
		"../../../../.env",
		"../../../../../../.env",
	}

	var data []byte
	var err error
	for _, path := range candidates {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err = readFileWithinRoot(path)
		if err == nil {
			break
		}
	}
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
				if err := os.Setenv(k, v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func readFileWithinRoot(path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)
	rootPath := filepath.Dir(cleanPath)
	fileName := filepath.Base(cleanPath)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(fileName)
	if closeErr := root.Close(); closeErr != nil {
		return data, errors.Join(readErr, closeErr)
	}
	return data, readErr
}

func testCliproxyBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("CLIPROXY_TEST_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("CLIPROXY_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "http://172.17.0.1:8787/v1"
	}

	return strings.Replace(baseURL, "host.docker.internal", "172.17.0.1", 1)
}

func newTestMemberNewsClient(t *testing.T) LLMClient {
	t.Helper()
	modelName := os.Getenv("CLIPROXY_TEST_MODEL")
	if modelName == "" {
		modelName = "gpt-5.4"
	}
	baseURL := testCliproxyBaseURL()

	t.Logf("Model: %s, BaseURL: %s (Chat Completions)", modelName, baseURL)

	return llm.NewClient(
		baseURL,
		os.Getenv("CLIPROXY_API_KEY"),
		modelName,
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		llm.WithSchemaName("member_news_summary"),
		llm.WithTemperature(0.3),
		llm.WithWebSearch(false),
		llm.WithChatCompletions(),
	)
}

func integrationCandidates() []model.FilteredCandidate {
	date := func(m, d int) time.Time {
		return time.Date(2026, time.Month(m), d, 12, 0, 0, 0, kst)
	}
	return []model.FilteredCandidate{
		{
			Candidate: model.Candidate{
				Title:       "hololive SUPER EXPO 2026",
				Description: "마쿠하리 메세에서 개최되는 홀로라이브 최대 규모 행사",
			},
			EffectiveDate:  date(3, 6),
			MatchedMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
			MemberText:     "사쿠라 미코, 호시마치 스이세이",
			Category:       model.CategoryEvent,
			SourceTier:     model.SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/events/expo2026",
		},
		{
			Candidate: model.Candidate{
				Title:       "Hoshimachi Suisei Live SuperNova: REBOOT",
				Description: "호시마치 스이세이 솔로 라이브 콘서트",
			},
			EffectiveDate:  date(2, 20),
			MatchedMembers: []string{"호시마치 스이세이"},
			MemberText:     "호시마치 스이세이",
			Category:       model.CategorySoloLive,
			SourceTier:     model.SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/events/suisei2026",
		},
		{
			Candidate: model.Candidate{
				Title:       "사쿠라 미코 생일 기념 굿즈",
				Description: "사쿠라 미코 생일 기념 한정 굿즈 판매",
			},
			EffectiveDate:  date(3, 5),
			MatchedMembers: []string{"사쿠라 미코"},
			MemberText:     "사쿠라 미코",
			Category:       model.CategoryGoods,
			SourceTier:     model.SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/miko-birthday-goods",
		},
		{
			Candidate: model.Candidate{
				Title:       "hololive DEV_IS NEW WAVE POP UP STORE",
				Description: "DEV_IS 소속 멤버 팝업스토어 개최",
			},
			EffectiveDate:  date(2, 14),
			MatchedMembers: []string{"사쿠라 미코"},
			MemberText:     "사쿠라 미코",
			Category:       model.CategoryOther,
			SourceTier:     model.SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/devis-popup",
		},
		{
			Candidate: model.Candidate{
				Title:       "ホロライブ バレンタイン 2026",
				Description: "HMV&BOOKS SHIBUYA 팝업 이벤트",
			},
			EffectiveDate:  date(2, 7),
			MatchedMembers: []string{"호시마치 스이세이"},
			MemberText:     "호시마치 스이세이",
			Category:       model.CategoryEvent,
			SourceTier:     model.SourceTierOfficial,
			SourceURL:      "https://hololive.hololivepro.com/news/valentine2026",
		},
	}
}

func TestIntegration_MemberNewsSummarize_Weekly(t *testing.T) {
	skipIfNoLLMKey(t)

	client := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(client, nil, validator, nil)

	input := model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), &input)
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

	input := model.SummarizeInput{
		Period:      model.PeriodMonthly,
		Now:         time.Date(2026, 3, 1, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), &input)
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
	if digest.Period != model.PeriodMonthly {
		t.Errorf("expected period %q, got %q", model.PeriodMonthly, digest.Period)
	}
}

func TestIntegration_MemberNewsSummarize_SchemaCompliance(t *testing.T) {
	skipIfNoLLMKey(t)

	client := newTestMemberNewsClient(t)
	validator := mustValidatorWithAllowlist(t)
	s := NewSummarizer(client, nil, validator, nil)

	input := model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"사쿠라 미코"},
		Candidates:  integrationCandidates(),
	}

	digest, err := s.Summarize(context.Background(), &input)
	if err != nil {
		t.Fatalf("summarize error: %v", err)
	}

	// period enum 검증
	if digest.Period != model.PeriodWeekly && digest.Period != model.PeriodMonthly {
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
		consensus.Config{
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		},
		nil,
	)

	input := model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"사쿠라 미코", "호시마치 스이세이"},
		Candidates:  integrationCandidates(),
	}

	digest, err := cs.Summarize(context.Background(), &input)
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

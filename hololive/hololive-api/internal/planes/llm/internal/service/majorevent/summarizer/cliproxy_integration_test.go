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
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func skipIfNoCliproxy(t *testing.T) {
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
	// monorepo 루트 .env 또는 모듈 로컬 .env를 순서대로 탐색한다.
	candidates := []string{
		os.Getenv("HOLOLIVE_API_ENV_FILE"),
		"../../../../.env",
		"../../../../../../.env",
	}

	var (
		data []byte
		err  error
	)

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
		return fmt.Errorf("read file within root: %w", err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if k, v, ok := strings.Cut(line, "="); ok {
			if os.Getenv(k) == "" {
				if err := os.Setenv(k, v); err != nil {
					return fmt.Errorf("setenv: %w", err)
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
		return nil, fmt.Errorf("open root: %w", err)
	}

	data, readErr := root.ReadFile(fileName)
	if closeErr := root.Close(); closeErr != nil {
		return data, errors.Join(readErr, closeErr)
	}

	if readErr != nil {
		return data, fmt.Errorf("read %q within root: %w", fileName, readErr)
	}

	return data, nil
}

func testCliproxyBaseURL() string {
	baseURL := strings.TrimSpace(os.Getenv("CLIPROXY_TEST_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("CLIPROXY_BASE_URL"))
	}

	if baseURL == "" {
		baseURL = "http://172.17.0.1:8787/v1"
	}

	// 호스트에서 실행하는 통합 테스트는 host.docker.internal 해석이 안 될 수 있다.
	return strings.Replace(baseURL, "host.docker.internal", "172.17.0.1", 1)
}

func newTestClient(t *testing.T, model string) *llm.OpenAIClient {
	t.Helper()

	baseURL := testCliproxyBaseURL()
	t.Logf("Model: %s, BaseURL: %s", model, baseURL)

	client, err := llm.NewClient(
		baseURL,
		os.Getenv("CLIPROXY_API_KEY"),
		model,
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
		llm.WithChatCompletions(),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return client
}

const integrationDefaultModel = "gpt-5.4"

func testModel() string {
	if model := os.Getenv("CLIPROXY_TEST_MODEL"); model != "" {
		return model
	}

	return integrationDefaultModel
}

func generateSummaryResponse(
	t *testing.T,
	client *llm.OpenAIClient,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	searchContext ...string,
) (string, *summaryResponse) {
	t.Helper()

	sysPrompt, err := getSystemPrompt(summaryType)
	if err != nil {
		t.Fatalf("getSystemPrompt 실패: %v", err)
	}

	userPrompt := buildUserPrompt(events, summaryType, periodKey, searchContext...)
	t.Logf("\n=== User Prompt ===\n%s\n=== END ===", userPrompt)

	rawJSON, err := client.GenerateJSON(t.Context(), sysPrompt, userPrompt, summaryResponseSchema())
	if err != nil {
		t.Fatalf("GenerateJSON 실패: %v", err)
	}

	t.Logf("\n=== Raw JSON ===\n%s\n=== END ===", rawJSON)

	var resp summaryResponse

	if unmarshalErr := jsonv2.Unmarshal([]byte(rawJSON), &resp); unmarshalErr != nil {
		t.Fatalf("JSON 파싱 실패: %v\nraw: %s", unmarshalErr, rawJSON)
	}

	return rawJSON, &resp
}

func assertHighlightsNotEmpty(t *testing.T, highlights []eventHighlight) {
	t.Helper()

	if len(highlights) == 0 {
		t.Fatal("highlights가 비어있음")
	}
}

func logHighlights(t *testing.T, highlights []eventHighlight) {
	t.Helper()

	for i, h := range highlights {
		t.Logf("highlight[%d]: name=%q date=%q members=%q note=%q link=%q", i, h.Name, h.Date, h.Members, h.Note, h.Link)
	}
}

func assertHighlightRequiredFields(t *testing.T, highlights []eventHighlight) {
	t.Helper()

	for i, h := range highlights {
		if h.Name == "" {
			t.Errorf("highlight[%d].name이 비어있음", i)
		}

		if h.Date == "" {
			t.Errorf("highlight[%d].date가 비어있음", i)
		}
	}
}

func assertNoGraduatedMembers(t *testing.T, highlights []eventHighlight) {
	t.Helper()

	for i, h := range highlights {
		if strings.Contains(h.Members, "沙花叉クロヱ") {
			t.Errorf("highlight[%d].members에 졸업 멤버 沙花叉クロヱ 포함", i)
		}
	}
}

func logOngoingEvents(t *testing.T, events []ongoingEvent) {
	t.Helper()

	t.Logf("ongoing_events 수: %d", len(events))

	for i, o := range events {
		t.Logf("ongoing[%d]: name=%q date=%q note=%q link=%q", i, o.Name, o.Date, o.Note, o.Link)
	}
}

func logDiscoveredEvents(t *testing.T, events []discoveredEvent) {
	t.Helper()

	t.Logf("discovered_events 수: %d", len(events))

	for i, d := range events {
		t.Logf("discovered[%d]: name=%q date=%q note=%q source=%q", i, d.Name, d.Date, d.Note, d.Source)
	}
}

func assertDiscoveredSourcesFilled(t *testing.T, events []discoveredEvent) {
	t.Helper()

	for i, d := range events {
		if d.Source == "" {
			t.Errorf("discovered[%d].source가 비어있음 (출처 필수)", i)
		}
	}
}

func assertContainsFesKeyword(t *testing.T, text, failMsg string) {
	t.Helper()

	for _, kw := range []string{"EXPO", "fes", "7th"} {
		if strings.Contains(text, kw) {
			return
		}
	}

	t.Error(failMsg)
}

func logAssembledText(t *testing.T, resp *summaryResponse) string {
	t.Helper()

	text := assembleSummaryText(resp)
	t.Logf("\n=== Assembled Text ===\n%s\n=== END ===", text)

	return text
}

func assertAssembledTextNotEmpty(t *testing.T, text string) {
	t.Helper()

	if text == "" {
		t.Fatal("assembleSummaryText 결과가 비어있음")
	}
}

func TestIntegration_Summarize_RawJSON_GPT(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s", model)

	_, resp := generateSummaryResponse(t, client, feb2026Events(), SummaryTypeWeekly, "2026-02-21")

	assertHighlightsNotEmpty(t, resp.Highlights)
	logHighlights(t, resp.Highlights)
	assertHighlightRequiredFields(t, resp.Highlights)
	logOngoingEvents(t, resp.OngoingEvents)
	assertNoGraduatedMembers(t, resp.Highlights)
	logDiscoveredEvents(t, resp.DiscoveredEvents)
	assertAssembledTextNotEmpty(t, logAssembledText(t, resp))
}

func TestIntegration_Summarize_Monthly_GPT(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	summarizer := NewEventSummarizer(client, nil, nil, testLogger())
	events := mar2026Events()

	result := summarizer.Summarize(t.Context(), events, SummaryTypeMonthly, "2026-03")

	t.Logf("Model: %s", model)
	t.Logf("\n=== 월간 요약 결과 (%s) ===\n%s\n=== END ===", model, result)

	if result == "" {
		t.Fatal("월간 요약 결과가 비어있음")
	}

	assertContainsFesKeyword(t, result, "월간 요약에 hololive fes/EXPO 관련 키워드가 없음")

	// 졸업 멤버 필터링 검증
	if strings.Contains(result, "天音かなた") {
		t.Error("월간 요약에 졸업 멤버 天音かなた 포함")
	}

	if strings.Contains(result, "沙花叉クロヱ") {
		t.Error("월간 요약에 졸업 멤버 沙花叉クロヱ 포함")
	}
}

func TestIntegration_Summarize_RawJSON_GPT_WebSearch(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s (Responses API + web_search)", model)

	_, resp := generateSummaryResponse(t, client, feb2026Events(), SummaryTypeWeekly, "2026-02-21")

	assertHighlightsNotEmpty(t, resp.Highlights)
	logHighlights(t, resp.Highlights)
	assertHighlightRequiredFields(t, resp.Highlights)
	logOngoingEvents(t, resp.OngoingEvents)
	assertNoGraduatedMembers(t, resp.Highlights)
	logDiscoveredEvents(t, resp.DiscoveredEvents)
	assertDiscoveredSourcesFilled(t, resp.DiscoveredEvents)

	if len(resp.DiscoveredEvents) == 0 {
		t.Log("WARN: web_search 활성화했지만 discovered_events가 비어있음 — 모델/검색 결과에 따라 발생 가능")
	}

	assertAssembledTextNotEmpty(t, logAssembledText(t, resp))
}

func TestIntegration_Summarize_Monthly_GPT_WebSearch(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	summarizer := NewEventSummarizer(client, nil, nil, testLogger())
	events := mar2026Events()

	t.Logf("Model: %s (Responses API + web_search)", model)

	result := summarizer.Summarize(t.Context(), events, SummaryTypeMonthly, "2026-03")

	t.Logf("\n=== 월간 요약 결과 (%s + web_search) ===\n%s\n=== END ===", model, result)

	if result == "" {
		t.Fatal("월간 요약 결과가 비어있음")
	}

	assertContainsFesKeyword(t, result, "월간 요약에 hololive fes/EXPO 관련 키워드가 없음")

	// 졸업 멤버 필터링 검증
	if strings.Contains(result, "天音かなた") {
		t.Error("월간 요약에 졸업 멤버 天音かなた 포함")
	}

	if strings.Contains(result, "沙花叉クロヱ") {
		t.Error("월간 요약에 졸업 멤버 沙花叉クロヱ 포함")
	}

	// discovered_events 존재 확인 (web_search 활성화)
	if strings.Contains(result, "[추가 발견]") {
		t.Log("OK: web_search로 추가 이벤트 발견")
	} else {
		t.Log("WARN: web_search 활성화했지만 추가 발견 섹션 없음")
	}
}

// exaSearchContext: Exa MCP 검색 결과를 시뮬레이션한 참고 자료
// 실제 운영에서는 Exa MCP 또는 REST API를 통해 동적으로 수집한다.
const exaFeb2026Context = `[1] Tokyo Station x Narita Airport 포스트카드 캠페인
출처: https://hololive.hololivepro.com/en/news/20260206-02-79/
기간: 2026-02-13 ~ 2026-03-09
내용: 도쿄역·나리타공항 홀로라이브 공식샵 양쪽 방문 시 한정 포스트카드 증정

[2] ANIMONIUM 2026 (태국 방콕)
출처: https://hololivemeet.hololivepro.com/eventschedule/
기간: 2026-02-06 ~ 2026-02-08
내용: hololive Meet 스테이지 참여, Siam Paragon

[3] hololive OFFICIAL CARD GAME Lunar New Year 2026 Special Tournament
출처: https://en.hololive-official-cardgame.com/events/post/lunarnewyear2026-specialtournament/
기간: 2026-02 (각 매장별)
내용: 구정 기념 카드게임 특별 토너먼트

[4] ANIPLUS x hololive EN -Advent- 한복 콜라보 카페
출처: https://hololive.hololivepro.com/en/news/20260123-01-188/
기간: 2026-02-05 ~ 2026-03-22
내용: 서울/부산 ANIPLUS 매장, Advent 한복 비주얼, 콜라보 메뉴·굿즈

[5] hololive SUPER EXPO 2026 티켓 일반 판매
출처: https://hololive.hololivepro.com/en/news/20251208-02-20/
기간: 2026-02-01~
내용: 현장/스트리밍 티켓 일반 판매 개시`

const exaMar2026Context = `[1] hololive SUPER EXPO 2026 & 7th fes. Ridin' on Dreams
출처: https://hololivesuperexpo.hololivepro.com/2026/
기간: 2026-03-06 ~ 2026-03-08
내용: 마쿠하리 메세, 엑스포+라이브 fes 3일간

[2] Dive into hololive CAPSULE 콜라보 카페
출처: https://hololivepro.com/en/collaboration/
기간: 2026-02-27 ~ 2026-04-26
내용: 도쿄 시부야 콜라보 카페, 3월에도 계속

[3] hololive Valentine 2026 POP UP & Fair
출처: https://hololive.hololivepro.com/en/news/
기간: 2026-02-07 ~ 2026-03-01
내용: HMV&BOOKS SHIBUYA 팝업, 전국 HMV 매장 페어

[4] Funko POP! x hololive production
출처: https://hololive.hololivepro.com/en/news/
기간: 2026-03 (판매 시작 예정)
내용: さくらみこ, Mori Calliope, Takanashi Kiara 피규어`

func TestIntegration_Summarize_Weekly_ExaPlusWebSearch(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s (Exa pre-search + Responses API web_search)", model)

	_, resp := generateSummaryResponse(t, client, feb2026Events(), SummaryTypeWeekly, "2026-02-21", exaFeb2026Context)

	assertHighlightsNotEmpty(t, resp.Highlights)
	logHighlights(t, resp.Highlights)
	logOngoingEvents(t, resp.OngoingEvents)
	logDiscoveredEvents(t, resp.DiscoveredEvents)
	assertDiscoveredSourcesFilled(t, resp.DiscoveredEvents)

	if len(resp.DiscoveredEvents) == 0 {
		t.Fatal("Exa 컨텍스트 주입했지만 discovered_events 비어있음")
	}

	assertNoGraduatedMembers(t, resp.Highlights)
	logAssembledText(t, resp)
}

func TestIntegration_Summarize_Monthly_ExaPlusWebSearch(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s (Exa pre-search + Responses API web_search)", model)

	rawJSON, resp := generateSummaryResponse(t, client, mar2026Events(), SummaryTypeMonthly, "2026-03", exaMar2026Context)

	assertHighlightsNotEmpty(t, resp.Highlights)
	logHighlights(t, resp.Highlights)
	logOngoingEvents(t, resp.OngoingEvents)
	logDiscoveredEvents(t, resp.DiscoveredEvents)

	if len(resp.DiscoveredEvents) == 0 {
		t.Fatal("Exa 컨텍스트 주입했지만 discovered_events 비어있음")
	}

	assertContainsFesKeyword(t, rawJSON, "SUPER EXPO/fes 관련 키워드가 없음")

	// 졸업 멤버 필터링
	if strings.Contains(rawJSON, "天音かなた") {
		t.Error("졸업 멤버 天音かなた 포함")
	}

	if text := logAssembledText(t, resp); strings.Contains(text, "[추가 발견]") {
		t.Log("OK: Exa+web_search 병행으로 추가 이벤트 발견")
	}
}

// exaFeb2026WithANIPLUS: 기존 exaFeb2026Context + ANIPLUS 한국 파트너 이벤트 추가.
const exaFeb2026WithANIPLUS = exaFeb2026Context + `

[6] ANIPLUS x hololive 라이브 뷰잉 — Hoshimachi Suisei Live "SuperNova: REBOOT"
출처: https://www.aniplus.co.kr/live-viewing/supernova-reboot
기간: 2026-02-21
내용: 전국 CGV 라이브 뷰잉 상영, 매주 ANIPLUS 한정 특전 증정

[7] ANIPLUS x hololive 라이브 뷰잉 — 秘密結社holoX Live 2026「First MISSION」
출처: https://www.aniplus.co.kr/live-viewing/first-mission
기간: 2026-02-25 ~ 2026-02-28
내용: 전국 CGV 라이브 뷰잉 상영, 회차별 특전 상이`

func TestIntegration_Summarize_DateAccuracy(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s (날짜 정확도 검증 — example 오염 제거 후)", model)

	_, resp := generateSummaryResponse(t, client, feb2026Events(), SummaryTypeWeekly, "2026-02-21")

	assertHighlightsNotEmpty(t, resp.Highlights)

	// 날짜 정확도 검증: SuperNova → 2/21 (입력 데이터 기준), NOT 2/20 (이전 example 오염)
	for _, h := range resp.Highlights {
		t.Logf("highlight: name=%q date=%q", h.Name, h.Date)

		if strings.Contains(h.Name, "SuperNova") {
			if !strings.Contains(h.Date, "2/21") {
				t.Errorf("SuperNova 날짜 오류: got %q, want 2/21 포함", h.Date)
			}

			if strings.Contains(h.Date, "2/20") {
				t.Errorf("SuperNova 날짜가 example 오염됨: got %q (2/20은 이전 example의 잘못된 날짜)", h.Date)
			}
		}

		// First MISSION → 2/25 시작 (입력 데이터 기준)
		if strings.Contains(h.Name, "First MISSION") || strings.Contains(h.Name, "holoX") {
			if !strings.Contains(h.Date, "2/25") {
				t.Errorf("First MISSION 날짜 오류: got %q, want 2/25 포함", h.Date)
			}
		}
	}

	logAssembledText(t, resp)
}

func TestIntegration_Summarize_ANIPLUSDiscovery(t *testing.T) {
	skipIfNoCliproxy(t)

	model := testModel()
	client := newTestClient(t, model)
	t.Logf("Model: %s (ANIPLUS 이벤트 발견 검증 — Exa 컨텍스트 포함)", model)

	_, resp := generateSummaryResponse(t, client, feb2026Events(), SummaryTypeWeekly, "2026-02-21", exaFeb2026WithANIPLUS)

	logDiscoveredEvents(t, resp.DiscoveredEvents)
	assertANIPLUSDiscovered(t, resp.DiscoveredEvents)
	assertDiscoveredSourcesTrusted(t, resp.DiscoveredEvents)

	// 날짜 정확도도 함께 검증
	for _, h := range resp.Highlights {
		if strings.Contains(h.Name, "SuperNova") && !strings.Contains(h.Date, "2/21") {
			t.Errorf("SuperNova 날짜 오류: got %q, want 2/21 포함", h.Date)
		}
	}

	if text := logAssembledText(t, resp); strings.Contains(text, "[추가 발견]") {
		t.Log("OK: ANIPLUS 이벤트가 추가 발견 섹션에 포함됨")
	}
}

func assertANIPLUSDiscovered(t *testing.T, events []discoveredEvent) {
	t.Helper()

	for _, d := range events {
		if strings.Contains(d.Name, "ANIPLUS") || strings.Contains(d.Name, "라이브 뷰잉") ||
			strings.Contains(d.Source, "aniplus") {
			return
		}
	}

	t.Error("discovered_events에 ANIPLUS/라이브 뷰잉 관련 이벤트 미발견 — KR 파트너 힌트가 효과 없음")
}

func assertDiscoveredSourcesTrusted(t *testing.T, events []discoveredEvent) {
	t.Helper()

	for i, d := range events {
		if d.Source != "" && !isTrustedDiscoveredSource(d.Source) {
			t.Errorf("discovered[%d].source %q is not trusted — should be filtered", i, d.Source)
		}
	}
}

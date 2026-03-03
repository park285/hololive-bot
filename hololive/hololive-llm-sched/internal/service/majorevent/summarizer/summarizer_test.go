package summarizer

import (
	"context"
	"fmt"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// --- assembleSummaryText 단위 테스트 ---

func TestAssembleSummaryText_WithHighlightsAndOngoing(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "hololive fes 2026", Date: "2/15(토)", Members: "", Note: "올해 최대 규모 페스티벌"},
			{Name: "白銀ノエル 생일 라이브", Date: "2/18(화)", Members: "白銀ノエル", Note: "단장님 생일 기념 솔로 라이브"},
		},
		OngoingEvents: []ongoingEvent{
			{Name: "hololive 카페 신주쿠점", Date: "2/1(토)~2/28(금)", Note: "카페 운영 중"},
			{Name: "굿즈샵 이케부쿠로", Date: "2/1(토)~3/15(토)", Note: "굿즈 판매 중"},
		},
	}

	result := assembleSummaryText(resp)

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	assertContains(t, result, "hololive fes 2026")
	assertContains(t, result, "白銀ノエル 생일 라이브")
	assertContains(t, result, "(白銀ノエル)")
	assertContains(t, result, "- 올해 최대 규모 페스티벌")
	assertContains(t, result, "[기간 행사]")
	assertContains(t, result, "hololive 카페 신주쿠점")
}

func TestAssembleSummaryText_HighlightsOnly(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트 이벤트"},
		},
	}

	result := assembleSummaryText(resp)
	assertContains(t, result, "3/1(토) Event A")
	assertContains(t, result, "- 테스트 이벤트")
	assertNotContains(t, result, "\n\n")
}

func TestAssembleSummaryText_NoMembers(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "콘서트", Date: "4/1(화)", Members: "", Note: "설명"},
		},
	}

	result := assembleSummaryText(resp)
	assertNotContains(t, result, "()")
}

func TestAssembleSummaryText_NilResponse(t *testing.T) {
	if result := assembleSummaryText(nil); result != "" {
		t.Errorf("expected empty string for nil, got %q", result)
	}
}

func TestAssembleSummaryText_EmptyHighlights(t *testing.T) {
	resp := &summaryResponse{Highlights: []eventHighlight{}}
	if result := assembleSummaryText(resp); result != "" {
		t.Errorf("expected empty string for empty highlights, got %q", result)
	}
}

func TestAssembleSummaryText_NoNote(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "이벤트", Date: "5/1(목)", Note: ""},
		},
	}

	result := assembleSummaryText(resp)
	assertContains(t, result, "5/1(목) 이벤트")
	assertNotContains(t, result, "- ") // note 없으면 하이픈 없음
}

// --- summaryResponseSchema 검증 ---

func TestSummaryResponseSchema_Structure(t *testing.T) {
	schema := summaryResponseSchema()

	if schema["type"] != "object" {
		t.Errorf("expected type=object, got %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}

	highlights, ok := props["highlights"].(map[string]any)
	if !ok {
		t.Fatal("missing highlights property")
	}
	if highlights["type"] != "array" {
		t.Errorf("highlights should be array, got %v", highlights["type"])
	}
	items, ok := highlights["items"].(map[string]any)
	if !ok {
		t.Fatal("highlights.items should not be nil")
	}

	itemProps, ok := items["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing item properties")
	}
	for _, required := range []string{"name", "date", "note"} {
		if _, ok := itemProps[required]; !ok {
			t.Errorf("missing required item property: %s", required)
		}
	}
	if _, ok := itemProps["link"]; !ok {
		t.Error("missing link property in highlight items")
	}

	ongoingEvents, ok := props["ongoing_events"].(map[string]any)
	if !ok {
		t.Error("missing ongoing_events property")
	}
	if ongoingEvents["type"] != "array" {
		t.Errorf("ongoing_events should be array, got %v", ongoingEvents["type"])
	}

	// OpenAI Responses API: properties의 모든 키가 required에 포함되어야 함
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("missing required array")
	}
	requiredSet := make(map[string]struct{}, len(required))
	for _, r := range required {
		requiredSet[r] = struct{}{}
	}
	for key := range props {
		if _, exists := requiredSet[key]; !exists {
			t.Errorf("property %q is in properties but missing from required", key)
		}
	}
}

// --- JSON 파싱 라운드트립 ---

func TestSummaryResponse_JSONRoundTrip(t *testing.T) {
	original := summaryResponse{
		Highlights: []eventHighlight{
			{Name: "fes 2026", Date: "2/15(토)", Members: "전원", Note: "최대 규모", Link: "https://example.com/fes"},
			{Name: "생일 라이브", Date: "2/18(화)", Members: "ノエル", Note: "솔로", Link: "https://example.com/noel"},
		},
		OngoingEvents: []ongoingEvent{
			{Name: "카페", Date: "2/1(토)~2/28(금)", Note: "운영 중", Link: "https://example.com/cafe"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed summaryResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(parsed.Highlights) != 2 {
		t.Errorf("expected 2 highlights, got %d", len(parsed.Highlights))
	}
	if parsed.Highlights[0].Link != "https://example.com/fes" {
		t.Errorf("highlight link mismatch: %q", parsed.Highlights[0].Link)
	}
	if len(parsed.OngoingEvents) != 1 {
		t.Errorf("expected 1 ongoing event, got %d", len(parsed.OngoingEvents))
	}
	if parsed.OngoingEvents[0].Link != "https://example.com/cafe" {
		t.Errorf("ongoing event link mismatch: %q", parsed.OngoingEvents[0].Link)
	}

	// 텍스트 조립까지 검증
	text := assembleSummaryText(&parsed)
	assertContains(t, text, "fes 2026")
	assertContains(t, text, "(전원)")
	assertContains(t, text, "https://example.com/fes")
	assertContains(t, text, "[기간 행사]")
	assertContains(t, text, "https://example.com/cafe")
}

// --- EventSummarizer + mock LLM 통합 ---

type mockSummarizer struct {
	jsonResponse string
	err          error
}

func (m *mockSummarizer) GenerateJSON(_ context.Context, _, _ string, _ map[string]any) (string, error) {
	return m.jsonResponse, m.err
}

func TestEventSummarizer_Summarize_StructuredOutput(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"hololive fes","date":"2/15(토)","members":"","note":"최대 규모 페스티벌","link":"https://example.com/fes"}],"ongoing_events":[{"name":"카페","date":"2/1(토)~2/28(금)","note":"카페 운영 중","link":""}],"discovered_events":[]}`

	mock := &mockSummarizer{jsonResponse: llmJSON}
	summarizer := NewEventSummarizer(mock, nil, nil, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "hololive fes", Link: "https://example.com/fes"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-02-15")

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	assertContains(t, result, "hololive fes")
	assertContains(t, result, "- 최대 규모 페스티벌")
	assertContains(t, result, "https://example.com/fes")
	assertContains(t, result, "[기간 행사]")
}

func TestEventSummarizer_Summarize_FinalOutputReviewApplied(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"Hoshimachi Suisei Live \"SuperNova: REBOOT\"","date":"2/20(금)","members":"星街すいせい","note":"솔로 라이브 공연","link":"https://hololive.hololivepro.com/events/supernova-reboot/"}],"ongoing_events":[],"discovered_events":[]}`

	primary := &mockSummarizer{jsonResponse: llmJSON}
	reviewer := &mockSummarizer{jsonResponse: `{"summary":"[리뷰 완료]\n2/20(금) Hoshimachi Suisei Live \"SuperNova: REBOOT\" (星街すいせい)\n- 솔로 라이브 공연\nhttps://hololive.hololivepro.com/events/supernova-reboot/"}`}

	summarizer := NewEventSummarizer(
		primary,
		nil,
		nil,
		testLogger(),
		WithSummarizerConsensus(reviewer, nil, SummarizerConsensusConfig{
			Enabled:             false, // JSON consensus는 비활성, final output review만 검증
			ConfidenceThreshold: 0.85,
			ReviewTimeout:       30 * time.Second,
			AdjudicateTimeout:   45 * time.Second,
		}),
	)

	events := []domain.MajorEvent{{ID: 1, Title: "Hoshimachi Suisei Live \"SuperNova: REBOOT\""}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-02-15")

	assertContains(t, result, "[리뷰 완료]")
	assertContains(t, result, "Hoshimachi Suisei Live")
}

func TestEventSummarizer_Summarize_InvalidJSON_ReturnsEmpty(t *testing.T) {
	mock := &mockSummarizer{jsonResponse: "not json"}
	summarizer := NewEventSummarizer(mock, nil, nil, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "test"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-02-15")

	if result != "" {
		t.Errorf("expected empty on invalid JSON, got %q", result)
	}
}

func TestEventSummarizer_Summarize_EmptyHighlights_ReturnsEmpty(t *testing.T) {
	mock := &mockSummarizer{jsonResponse: `{"highlights":[]}`}
	summarizer := NewEventSummarizer(mock, nil, nil, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "test"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-02-15")

	if result != "" {
		t.Errorf("expected empty on empty highlights, got %q", result)
	}
}

func TestEventSummarizer_Summarize_NilLLM_ReturnsEmpty(t *testing.T) {
	summarizer := NewEventSummarizer(nil, nil, nil, testLogger())

	events := []domain.MajorEvent{{ID: 1}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "key")

	if result != "" {
		t.Errorf("expected empty for nil LLM, got %q", result)
	}
}

func TestEventSummarizer_Summarize_NoEvents_ReturnsEmpty(t *testing.T) {
	mock := &mockSummarizer{jsonResponse: "should not be called"}
	summarizer := NewEventSummarizer(mock, nil, nil, testLogger())

	result := summarizer.Summarize(context.Background(), nil, SummaryTypeWeekly, "key")
	if result != "" {
		t.Errorf("expected empty for nil events, got %q", result)
	}
}

// --- 링크 렌더링 테스트 ---

func TestAssembleSummaryText_HighlightWithLink(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Suisei Live", Date: "2/20(금)", Members: "星街すいせい", Note: "솔로 라이브 공연", Link: "https://hololivepro.com/events/123"},
		},
	}
	result := assembleSummaryText(resp)
	assertContains(t, result, "https://hololivepro.com/events/123")
	assertContains(t, result, "- 솔로 라이브 공연")
}

func TestAssembleSummaryText_HighlightWithoutLink(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트", Link: ""},
		},
	}
	result := assembleSummaryText(resp)
	assertContains(t, result, "3/1(토) Event A")
	assertNotContains(t, result, "https://")
}

func TestAssembleSummaryText_OngoingEventsWithLink(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트"},
		},
		OngoingEvents: []ongoingEvent{
			{Name: "도쿄역 포스트카드 이벤트", Date: "2/12(목)~3/8(일)", Note: "한정 포스트카드 증정", Link: "https://hololivepro.com/news/456"},
		},
	}
	result := assembleSummaryText(resp)
	assertContains(t, result, "[기간 행사]")
	assertContains(t, result, "https://hololivepro.com/news/456")
	assertContains(t, result, "도쿄역 포스트카드 이벤트")
}

func TestAssembleSummaryText_DiscoveredWithSource(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트"},
		},
		DiscoveredEvents: []discoveredEvent{
			{Name: "콜라보 카페", Date: "2/5(목)~3/22(일)", Note: "서울·부산 콜라보 카페 운영", Source: "https://hololive.hololivepro.com/en/news/20260123-01-188/"},
		},
	}
	result := assembleSummaryText(resp)
	assertContains(t, result, "[추가 발견]")
	assertContains(t, result, "https://hololive.hololivepro.com/en/news/20260123-01-188/")
}

func TestAssembleSummaryText_NoOngoingSectionWhenEmpty(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트"},
		},
		OngoingEvents: []ongoingEvent{},
	}
	result := assembleSummaryText(resp)
	assertNotContains(t, result, "[기간 행사]")
}

func TestEventSummarizer_Summarize_OldOngoingNoteIgnored(t *testing.T) {
	// 구형 응답의 ongoing_note는 더 이상 텍스트 조립에 반영하지 않는다.
	llmJSON := `{"highlights":[{"name":"Event A","date":"3/1(토)","members":"","note":"테스트","link":""}],"ongoing_note":"카페 (~2/28) 진행 중","discovered_events":[]}`

	mock := &mockSummarizer{jsonResponse: llmJSON}
	summarizer := NewEventSummarizer(mock, nil, nil, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "Event A"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

	if result == "" {
		t.Fatal("expected non-empty result")
	}
	assertContains(t, result, "Event A")
	assertNotContains(t, result, "카페 (~2/28) 진행 중")
	assertNotContains(t, result, "[기간 행사]")
}

func TestAssembleSummaryText_SyntheticOngoingEvent(t *testing.T) {
	// 월간 10건 초과 시 생성되는 synthetic "기타 행사" 항목 (date 비어있음)
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "3/1(토)", Note: "테스트"},
		},
		OngoingEvents: []ongoingEvent{
			{Name: "카페", Date: "2/1(토)~2/28(금)", Note: "카페 운영 중", Link: "https://example.com/cafe"},
			{Name: "기타 행사", Date: "", Note: "외 3건의 행사 예정", Link: ""},
		},
	}
	result := assembleSummaryText(resp)
	assertContains(t, result, "[기간 행사]")
	assertContains(t, result, "2/1(토)~2/28(금) 카페")
	assertContains(t, result, "기타 행사")
	assertContains(t, result, "- 외 3건의 행사 예정")
	// synthetic 항목: date 비어있으면 줄바꿈 직후 name (date+space prefix 없음)
	assertContains(t, result, "\n기타 행사")
}

func TestIsTrustedDiscoveredSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "official domain",
			source: "https://hololive.hololivepro.com/events/supernova-reboot/",
			want:   true,
		},
		{
			name:   "korea partner x account",
			source: "https://x.com/ANIPLUS_SHOP/status/2023276387621130436",
			want:   true,
		},
		{
			name:   "partner domain",
			source: "https://www.aniplus.co.kr/event/detail?idx=1",
			want:   true,
		},
		{
			name:   "official holostars account",
			source: "https://x.com/HOLOSTARSen/status/1",
			want:   true,
		},
		{
			name:   "korea event partner account",
			source: "https://twitter.com/v_square_kr/status/1",
			want:   true,
		},
		{
			name:   "untrusted fan account",
			source: "https://x.com/hololive_fan_news/status/1",
			want:   false,
		},
		{
			name:   "removed unverified account",
			source: "https://x.com/hololive_goods/status/1",
			want:   false,
		},
		{
			name:   "lookalike domain",
			source: "https://hololivepro.com.evil.example.com/event",
			want:   false,
		},
		{
			name:   "bare domain rejected",
			source: "hololivepro.com",
			want:   false,
		},
		{
			name:   "bare social account rejected",
			source: "hololivetv",
			want:   false,
		},
		{
			name:   "at-prefixed social account accepted",
			source: "@hololivetv",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTrustedDiscoveredSource(tt.source)
			if got != tt.want {
				t.Fatalf("isTrustedDiscoveredSource(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestAssembleSummaryText_HighlightsSeparatedByBlankLine(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{
			{Name: "Event A", Date: "2/20(목)", Note: "노트A"},
			{Name: "Event B", Date: "2/21(금)", Note: "노트B"},
		},
	}
	result := assembleSummaryText(resp)
	if !strings.Contains(result, "\n\n") {
		t.Errorf("highlights should be separated by blank line, got: %q", result)
	}
}

func TestWriteDiscoveredEvents_HasSourcePrefix(t *testing.T) {
	resp := &summaryResponse{
		Highlights: []eventHighlight{{Name: "H", Date: "1/1(수)", Note: "n"}},
		DiscoveredEvents: []discoveredEvent{
			{Name: "D", Date: "2/20(목)", Note: "n", Source: "https://example.com"},
		},
	}
	result := assembleSummaryText(resp)
	if !strings.Contains(result, "출처: ") {
		t.Errorf("discovered events should have '출처: ' prefix, got: %q", result)
	}
}

// --- 헬퍼 ---

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !containsStr(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if containsStr(s, substr) {
		t.Errorf("expected %q NOT to contain %q", s, substr)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- promptVersion / example 오염 제거 테스트 ---

// mockCache: 캐시 키 캡처용 mock (캐시 키 버전 포함 여부 검증)
type mockCache struct {
	getKey  string
	setKey  string
	getData any
	getErr  error
	setErr  error
}

func (m *mockCache) Get(_ context.Context, key string, dest any) error {
	m.getKey = key
	if m.getErr != nil {
		return m.getErr
	}
	if m.getData != nil {
		// 캐시 히트 시뮬레이션
		if s, ok := m.getData.(string); ok {
			if p, ok := dest.(*string); ok {
				*p = s
			}
		}
	}
	return nil
}

func (m *mockCache) Set(_ context.Context, key string, _ any, _ time.Duration) error {
	m.setKey = key
	return m.setErr
}

// TestSystemPrompt_NoRealEventNamesInExample: example 블록에 실제 이벤트명이 없는지 확인
func TestSystemPrompt_NoRealEventNamesInExample(t *testing.T) {
	tests := []struct {
		name       string
		promptType SummaryType
	}{
		{"weekly", SummaryTypeWeekly},
		{"monthly", SummaryTypeMonthly},
	}

	forbiddenNames := []string{"SuperNova", "First MISSION"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := getSystemPrompt(tt.promptType)

			// <example> 블록만 추출
			exampleStart := strings.Index(prompt, "<example>")
			exampleEnd := strings.Index(prompt, "</example>")
			if exampleStart == -1 || exampleEnd == -1 {
				t.Fatal("example block not found in prompt")
			}
			exampleBlock := prompt[exampleStart : exampleEnd+len("</example>")]

			for _, name := range forbiddenNames {
				if strings.Contains(exampleBlock, name) {
					t.Errorf("example block contains real event name %q — this causes LLM date contamination", name)
				}
			}
		})
	}
}

// TestBuildUserPrompt_EventDatePassthrough: 입력 이벤트 날짜가 user prompt에 올바르게 포함되는지 확인
func TestBuildUserPrompt_EventDatePassthrough(t *testing.T) {
	startDate := time.Date(2026, 2, 21, 0, 0, 0, 0, kst)
	events := []domain.MajorEvent{
		{
			ID:             1,
			Title:          "Test Event",
			EventStartDate: &startDate,
		},
	}

	prompt := buildUserPrompt(events, SummaryTypeWeekly, "2026-02-21")

	if !strings.Contains(prompt, "2026년 2월 21일") {
		t.Errorf("user prompt should contain formatted date '2026년 2월 21일', got: %s", prompt)
	}
}

// TestSummarize_CacheKeyContainsPromptVersion: 캐시 키에 promptVersion이 포함되는지 확인
func TestSummarize_CacheKeyContainsPromptVersion(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"Test","date":"3/1(토)","members":"","note":"test","link":""}],"ongoing_events":[],"discovered_events":[]}`

	t.Run("cache set key contains promptVersion", func(t *testing.T) {
		cache := &mockCache{getErr: fmt.Errorf("cache miss")}
		mock := &mockSummarizer{jsonResponse: llmJSON}
		summarizer := NewEventSummarizer(mock, cache, nil, testLogger())

		events := []domain.MajorEvent{{ID: 1, Title: "Test"}}
		summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

		if !strings.Contains(cache.setKey, promptVersion) {
			t.Errorf("cache set key %q should contain promptVersion %q", cache.setKey, promptVersion)
		}
	})

	t.Run("cache get key contains promptVersion", func(t *testing.T) {
		cache := &mockCache{getErr: fmt.Errorf("cache miss")}
		mock := &mockSummarizer{jsonResponse: llmJSON}
		summarizer := NewEventSummarizer(mock, cache, nil, testLogger())

		events := []domain.MajorEvent{{ID: 1, Title: "Test"}}
		summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

		if !strings.Contains(cache.getKey, promptVersion) {
			t.Errorf("cache get key %q should contain promptVersion %q", cache.getKey, promptVersion)
		}
	})

	t.Run("unversioned cache key does not hit", func(t *testing.T) {
		// unversioned key로 pre-seed된 캐시가 hit되지 않는 것 확인
		cache := &mockCache{getErr: fmt.Errorf("cache miss")}
		mock := &mockSummarizer{jsonResponse: llmJSON}
		summarizer := NewEventSummarizer(mock, cache, nil, testLogger())

		events := []domain.MajorEvent{{ID: 1, Title: "Test"}}
		result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

		// LLM이 호출되어야 함 (캐시 miss이므로)
		if result == "" {
			t.Error("expected non-empty result from LLM when cache misses")
		}
		// set key에 version 포함 확인
		expectedPrefix := fmt.Sprintf("majorevent:summary:%s:", promptVersion)
		if !strings.HasPrefix(cache.setKey, expectedPrefix) {
			t.Errorf("cache set key %q should start with %q", cache.setKey, expectedPrefix)
		}
	})
}

// mockSearcher: 검색 결과 mock (병렬 호출 안전 — 쿼리 문자열로 1차/2차 분기)
type mockSearcher struct {
	results   []SearchResult
	err       error
	mu        sync.Mutex
	callCount int
	// KR 쿼리("ANIPLUS" 포함) 시 다른 결과 반환
	krResults []SearchResult
	krErr     error
}

func (m *mockSearcher) Search(_ context.Context, query string) ([]SearchResult, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	// KR 파트너 쿼리 판별: "live viewing" 키워드로 분기 (1차 범용 쿼리에도 "ANIPLUS"가 포함되므로)
	if strings.Contains(query, "live viewing") && (m.krResults != nil || m.krErr != nil) {
		return m.krResults, m.krErr
	}
	return m.results, m.err
}

func TestSystemPrompt_ContainsKRPartnerHint(t *testing.T) {
	tests := []struct {
		name       string
		promptType SummaryType
	}{
		{"weekly", SummaryTypeWeekly},
		{"monthly", SummaryTypeMonthly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := getSystemPrompt(tt.promptType)
			if !strings.Contains(prompt, "Korean partner events") && !strings.Contains(prompt, "ANIPLUS") {
				t.Error("prompt should contain Korean partner hint (ANIPLUS or Korean partner events)")
			}
		})
	}
}

func TestSummarize_KRSearchFailure_GracefulDegradation(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"Test Event","date":"3/1(토)","members":"","note":"test","link":""}],"ongoing_events":[],"discovered_events":[]}`

	searcher := &mockSearcher{
		results: []SearchResult{{Title: "Primary Result", URL: "https://example.com/1"}},
		krErr:   fmt.Errorf("KR search timeout"),
	}
	mock := &mockSummarizer{jsonResponse: llmJSON}
	summarizer := NewEventSummarizer(mock, nil, searcher, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "Test Event"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

	if result == "" {
		t.Fatal("expected non-empty result when KR search fails but primary succeeds")
	}
	assertContains(t, result, "Test Event")
}

func TestSummarize_PrimarySearchFailure_UsesKRResults(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"Test Event","date":"3/1(토)","members":"","note":"test","link":""}],"ongoing_events":[],"discovered_events":[]}`

	searcher := &mockSearcher{
		err:       fmt.Errorf("primary search timeout"),
		krResults: []SearchResult{{Title: "KR Result", URL: "https://aniplus.co.kr/1"}},
	}
	mock := &mockSummarizer{jsonResponse: llmJSON}
	summarizer := NewEventSummarizer(mock, nil, searcher, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "Test Event"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

	if result == "" {
		t.Fatal("expected non-empty result when primary search fails but KR succeeds")
	}
	assertContains(t, result, "Test Event")
}

func TestSummarize_DualSearch_MergeOrder(t *testing.T) {
	llmJSON := `{"highlights":[{"name":"Test","date":"3/1(토)","members":"","note":"test","link":""}],"ongoing_events":[],"discovered_events":[]}`

	// 1차 5건
	primary := make([]SearchResult, 5)
	for i := range primary {
		primary[i] = SearchResult{Title: fmt.Sprintf("Primary %d", i), URL: fmt.Sprintf("https://primary.com/%d", i)}
	}

	// 2차 8건 (중복 2건)
	secondary := make([]SearchResult, 8)
	for i := range secondary {
		if i < 2 {
			// 1차와 중복
			secondary[i] = SearchResult{Title: fmt.Sprintf("Primary %d", i), URL: fmt.Sprintf("https://primary.com/%d", i)}
		} else {
			secondary[i] = SearchResult{Title: fmt.Sprintf("KR %d", i), URL: fmt.Sprintf("https://kr.com/%d", i)}
		}
	}

	searcher := &mockSearcher{
		results:   primary,
		krResults: secondary,
	}
	mock := &mockSummarizer{jsonResponse: llmJSON}
	summarizer := NewEventSummarizer(mock, nil, searcher, testLogger())

	events := []domain.MajorEvent{{ID: 1, Title: "Test"}}
	result := summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-03-01")

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// 검증: 병합 파이프라인 (5 + 8 - 2중복 = 11 → cap 10)
	// 직접 검증은 어려우므로 요약 생성 성공만 확인
	assertContains(t, result, "Test")

	// mock searcher callCount가 2여야 함 (1차 + 2차)
	if searcher.callCount != 2 {
		t.Errorf("expected 2 search calls, got %d", searcher.callCount)
	}
}

// TestRunDualSearch_DirectVerification: runDualSearch의 dedupe/cap/순서 직접 검증
func TestRunDualSearch_DirectVerification(t *testing.T) {
	t.Run("deduplication and cap", func(t *testing.T) {
		// 1차 5건 + 2차 8건 (중복 2건) → dedupe 11건 → cap 10건
		primary := make([]SearchResult, 5)
		for i := range primary {
			primary[i] = SearchResult{Title: fmt.Sprintf("P%d", i), URL: fmt.Sprintf("https://p.com/%d", i)}
		}
		secondary := make([]SearchResult, 8)
		for i := range secondary {
			if i < 2 {
				secondary[i] = SearchResult{Title: fmt.Sprintf("P%d", i), URL: fmt.Sprintf("https://p.com/%d", i)}
			} else {
				secondary[i] = SearchResult{Title: fmt.Sprintf("K%d", i), URL: fmt.Sprintf("https://k.com/%d", i)}
			}
		}

		searcher := &mockSearcher{results: primary, krResults: secondary}
		s := NewEventSummarizer(nil, nil, searcher, testLogger())

		result := s.runDualSearch(context.Background(), SummaryTypeWeekly, "2026-03-01")

		if result == "" {
			t.Fatal("expected non-empty search context")
		}

		// 1차 결과가 2차보다 앞에 위치해야 함
		p0Pos := strings.Index(result, "P0")
		k2Pos := strings.Index(result, "K2")
		if p0Pos == -1 || k2Pos == -1 {
			t.Fatalf("expected P0 and K2 in result, got: %s", result)
		}
		if p0Pos >= k2Pos {
			t.Errorf("primary results should precede KR results: P0@%d >= K2@%d", p0Pos, k2Pos)
		}

		// cap 10 적용 확인: [1]~[10] 존재, [11] 부재
		if !strings.Contains(result, "[10]") {
			t.Error("expected [10] marker (10th result)")
		}
		if strings.Contains(result, "[11]") {
			t.Error("should not have [11] marker (cap at 10)")
		}
	})

	t.Run("nil searcher returns empty", func(t *testing.T) {
		s := NewEventSummarizer(nil, nil, nil, testLogger())
		result := s.runDualSearch(context.Background(), SummaryTypeWeekly, "2026-03-01")
		if result != "" {
			t.Errorf("expected empty for nil searcher, got %q", result)
		}
	})

	t.Run("both searches fail returns empty", func(t *testing.T) {
		searcher := &mockSearcher{
			err:   fmt.Errorf("primary fail"),
			krErr: fmt.Errorf("kr fail"),
		}
		s := NewEventSummarizer(nil, nil, searcher, testLogger())
		result := s.runDualSearch(context.Background(), SummaryTypeWeekly, "2026-03-01")
		if result != "" {
			t.Errorf("expected empty when both searches fail, got %q", result)
		}
	})
}

// TestSystemPrompt_ContainsDateAuthority: 시스템 프롬프트에 date_authority 블록이 포함되는지 확인
func TestSystemPrompt_ContainsDateAuthority(t *testing.T) {
	tests := []struct {
		name       string
		promptType SummaryType
	}{
		{"weekly", SummaryTypeWeekly},
		{"monthly", SummaryTypeMonthly},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := getSystemPrompt(tt.promptType)

			if !strings.Contains(prompt, "date_authority") {
				t.Error("prompt should contain date_authority block")
			}
			if !strings.Contains(prompt, "NEVER copy dates from examples") {
				t.Error("prompt should contain 'NEVER copy dates from examples' instruction")
			}
		})
	}
}

// TestGraduatedMembersLoad: JSON 파싱 성공 + domainContext에 졸업 멤버 포함 확인
func TestGraduatedMembersLoad(t *testing.T) {
	dc := getDomainContext()
	if !strings.Contains(dc, "天音かなた") {
		t.Error("domainContext should contain graduated member 天音かなた")
	}
	if !strings.Contains(dc, "Gawr Gura") {
		t.Error("domainContext should contain graduated member Gawr Gura")
	}
	if !strings.Contains(dc, "hololive CN") {
		t.Error("domainContext should mention dissolved hololive CN branch")
	}
	// 전체 졸업 멤버 수 > 0 확인
	total := 0
	for _, members := range parsedGraduatedData.Graduated {
		total += len(members)
	}
	if total == 0 {
		t.Error("expected at least one graduated member")
	}
}

// TestNoteMaxLength: 스키마에 maxLength 존재 확인
func TestNoteMaxLength(t *testing.T) {
	schema := summaryResponseSchema()
	props := schema["properties"].(map[string]any)
	highlights := props["highlights"].(map[string]any)
	items := highlights["items"].(map[string]any)
	itemProps := items["properties"].(map[string]any)
	note := itemProps["note"].(map[string]any)
	if note["maxLength"] != 30 {
		t.Errorf("highlight note maxLength should be 30, got %v", note["maxLength"])
	}
}

// TestNoteTruncation: 31자 이상 note → 30자+… 트렁케이션 확인
func TestNoteTruncation(t *testing.T) {
	// 30 rune 입력 — 트렁케이션 없음
	input := "가나다라마바사아자차카타파하가나다라마바사아자차카타파하가나" // 30자
	if truncateNote(input, 30) != input {
		t.Error("30-char input should not be truncated")
	}

	// 31 rune 입력 — 트렁케이션 적용
	longInput := input + "X" // 31자
	result := truncateNote(longInput, 30)
	runes := []rune(result)
	if len(runes) != 31 { // 30 + "…" (1 rune)
		t.Errorf("expected 31 runes (30+…), got %d", len(runes))
	}
	if !strings.HasSuffix(result, "…") {
		t.Errorf("truncated result should end with …, got %q", result)
	}
}

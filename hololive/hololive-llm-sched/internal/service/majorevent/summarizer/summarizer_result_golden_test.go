package summarizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestEventSummarizer_Summarize_WeeklyMatchesGolden(t *testing.T) {
	t.Parallel()

	llmJSON := `{"highlights":[{"name":"hololive fes","date":"2/15(토)","members":"","note":"최대 규모 페스티벌","link":"https://example.com/fes"}],"ongoing_events":[{"name":"카페","date":"2/1(토)~2/28(금)","note":"카페 운영 중","link":"https://example.com/cafe"}],"discovered_events":[{"name":"추가 발표","date":"2/21(토)","note":"검색으로 발견된 추가 일정","source":"hololivepro.com/news","link":"https://hololivepro.com/news/extra"}]}`

	summarizer := NewEventSummarizer(&mockSummarizer{jsonResponse: llmJSON}, nil, nil, testLogger())
	events := []domain.MajorEvent{{ID: 1, Title: "hololive fes", Link: "https://example.com/fes"}}

	assertSummaryGolden(t,
		"weekly_summary_result.golden.txt",
		summarizer.Summarize(context.Background(), events, SummaryTypeWeekly, "2026-02-15"),
	)
}

func TestEventSummarizer_Summarize_MonthlyMatchesGolden(t *testing.T) {
	t.Parallel()

	llmJSON := `{"highlights":[{"name":"Spring Concert","date":"3/20(금)","members":"Member A, Member B","note":"월간 대표 콘서트","link":"https://example.com/concert"}],"ongoing_events":[{"name":"POP UP STORE","date":"3/1(일)~3/31(화)","note":"월간 팝업 스토어","link":"https://example.com/store"}],"discovered_events":[]}`

	summarizer := NewEventSummarizer(&mockSummarizer{jsonResponse: llmJSON}, nil, nil, testLogger())
	events := []domain.MajorEvent{{ID: 1, Title: "Spring Concert", Link: "https://example.com/concert"}}

	assertSummaryGolden(t,
		"monthly_summary_result.golden.txt",
		summarizer.Summarize(context.Background(), events, SummaryTypeMonthly, "2026-03"),
	)
}

func assertSummaryGolden(t *testing.T, name, text string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("golden 디렉터리 생성 실패: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(text), 0o644); err != nil {
			t.Fatalf("golden 파일 갱신 실패: %v", err)
		}
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden 파일 읽기 실패: %v", err)
	}

	if string(golden) != text {
		t.Fatalf("summary golden mismatch for %s (갱신하려면 UPDATE_GOLDEN=1 사용)", name)
	}
}

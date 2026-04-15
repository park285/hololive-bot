package summarizer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemPrompt_WeeklyMatchesGolden(t *testing.T) {
	t.Parallel()

	assertPromptMatchesGolden(t, SummaryTypeWeekly, "weekly_system_prompt.golden.txt")
}

func TestSystemPrompt_MonthlyMatchesGolden(t *testing.T) {
	t.Parallel()

	assertPromptMatchesGolden(t, SummaryTypeMonthly, "monthly_system_prompt.golden.txt")
}

func assertPromptMatchesGolden(t *testing.T, summaryType SummaryType, goldenName string) {
	t.Helper()

	prompt, err := getSystemPrompt(summaryType)
	if err != nil {
		t.Fatalf("getSystemPrompt(%s) 실패: %v", summaryType, err)
	}

	goldenPath := filepath.Join("testdata", goldenName)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("golden 디렉터리 생성 실패: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(prompt), 0o644); err != nil {
			t.Fatalf("golden 파일 갱신 실패: %v", err)
		}
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden 파일 읽기 실패: %v", err)
	}

	if string(golden) != prompt {
		t.Fatalf("system prompt golden mismatch for %s (갱신하려면 UPDATE_GOLDEN=1 사용)", summaryType)
	}
}

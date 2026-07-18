package model_test

import (
	"testing"
	"time"

	model "github.com/kapu/hololive-api/internal/planes/llm/internal/model"
)

func TestWeekdayKRIndexesByTimeWeekday(t *testing.T) {
	if got := len(model.WeekdayKR); got != 7 {
		t.Fatalf("len(WeekdayKR) = %d, want 7", got)
	}

	for weekday, want := range map[time.Weekday]string{
		time.Sunday:    "일",
		time.Monday:    "월",
		time.Saturday:  "토",
		time.Wednesday: "수",
	} {
		if got := model.WeekdayKR[weekday]; got != want {
			t.Errorf("WeekdayKR[%s] = %q, want %q", weekday, got, want)
		}
	}
}

func TestSummaryResultTypeWireValues(t *testing.T) {
	for value, want := range map[model.SummaryResultType]string{
		model.SummaryResultPrimary:  "primary",
		model.SummaryResultFallback: "fallback",
		model.SummaryResultEmpty:    "empty",
	} {
		if string(value) != want {
			t.Errorf("SummaryResultType = %q, want %q", value, want)
		}
	}
}

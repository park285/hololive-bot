package model_test

import (
	"testing"

	model "github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

func TestNormalizePeriod(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input model.Period
		want  model.Period
	}{
		{name: "monthly keyword", input: "monthly", want: model.PeriodMonthly},
		{name: "korean monthly alias", input: "이번달", want: model.PeriodMonthly},
		{name: "korean monthly label", input: "월간", want: model.PeriodMonthly},
		{name: "uppercase with whitespace", input: " MONTHLY ", want: model.PeriodMonthly},
		{name: "weekly keyword", input: "weekly", want: model.PeriodWeekly},
		{name: "unknown falls back to weekly", input: "yearly", want: model.PeriodWeekly},
		{name: "empty falls back to weekly", input: "", want: model.PeriodWeekly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.NormalizePeriod(tc.input); got != tc.want {
				t.Fatalf("NormalizePeriod(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestDefaultHeadlineFollowsNormalizedPeriod(t *testing.T) {
	if got := model.DefaultHeadline("이번달"); got != "📅 이번달 구독 멤버 뉴스" {
		t.Fatalf("DefaultHeadline(이번달) = %q, want monthly headline", got)
	}
	if got := model.DefaultHeadline("unknown"); got != "🗞️ 이번주 구독 멤버 뉴스" {
		t.Fatalf("DefaultHeadline(unknown) = %q, want weekly headline", got)
	}
}

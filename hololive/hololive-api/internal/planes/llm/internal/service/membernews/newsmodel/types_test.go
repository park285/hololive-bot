package newsmodel_test

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/newsmodel"
)

func TestDefaultHeadlineFollowsNormalizedPeriod(t *testing.T) {
	if got := newsmodel.DefaultHeadline("이번달"); got != "📅 이번달 구독 멤버 뉴스" {
		t.Fatalf("DefaultHeadline(이번달) = %q, want monthly headline", got)
	}

	if got := newsmodel.DefaultHeadline("unknown"); got != "🗞️ 이번주 구독 멤버 뉴스" {
		t.Fatalf("DefaultHeadline(unknown) = %q, want weekly headline", got)
	}
}

package render

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type liveStringCase struct {
	name string
	got  func(*liveMetrics) string
	want string
}

func liveStringCases() []liveStringCase {
	return []liveStringCase{
		{"header", func(m *liveMetrics) string { return m.liveHeaderText() }, "현재 라이브"},
		{"summary", func(m *liveMetrics) string { return m.liveSummaryText(5) }, "총 5건"},
		{"badge_chzzk", func(m *liveMetrics) string { return m.liveChzzkBadge() }, "치지직"},
		{"overflow_footer", func(m *liveMetrics) string { return m.liveOverflowText(4) }, "외 4건 생략"},
	}
}

func TestLiveStrings_NilStoreFallbackByteEqual(t *testing.T) {
	t.Parallel()

	m := newLiveMetrics()
	for _, c := range liveStringCases() {
		if got := c.got(&m); got != c.want {
			t.Errorf("%s nil-store = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLiveStrings_SeededStoreByteEqual(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	m := newLiveMetrics()
	m.strings = store

	for _, c := range liveStringCases() {
		if got := c.got(&m); got != c.want {
			t.Errorf("%s seeded-store = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLiveStrings_SeededRowsMatchFallbackLiterals(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	cases := []struct {
		key      string
		fallback string
	}{
		{"header", "현재 라이브"},
		{"summary", "총 %d건"},
		{"badge_chzzk", "치지직"},
		{"overflow_footer", "외 %d건 생략"},
	}
	for _, c := range cases {
		got := store.Get(messagestrings.NamespaceLiveCard, c.key)
		if got != c.fallback {
			t.Errorf("seeded livecard/%s = %q, want %q (must match code fallback byte-for-byte)", c.key, got, c.fallback)
		}
	}
}

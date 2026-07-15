package render

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type calendarStringCase struct {
	name string
	got  func(context.Context, *calendarMetrics) string
	want string
}

func calendarStringCases() []calendarStringCase {
	return []calendarStringCase{
		{"header_month", func(ctx context.Context, m *calendarMetrics) string { return m.headerText(ctx, 2026, 6) }, "2026년 6월 기념일"},
		{"summary", func(ctx context.Context, m *calendarMetrics) string { return m.summaryText(ctx, 5, 3, 2) }, "총 5건 · 생일 3 · 데뷔주년 2"},
		{"empty", func(ctx context.Context, m *calendarMetrics) string { return m.emptyText(ctx) }, "등록된 기념일이 없습니다."},
		{"day", func(ctx context.Context, m *calendarMetrics) string { return m.dayText(ctx, 6, 15) }, "6월 15일"},
		{"badge_birthday", func(ctx context.Context, m *calendarMetrics) string { return m.badgeBirthday(ctx) }, "생일"},
		{"badge_anniversary", func(ctx context.Context, m *calendarMetrics) string { return m.anniversaryBadge(ctx, 3) }, "데뷔 3주년"},
		{"unknown", func(ctx context.Context, m *calendarMetrics) string { return m.unknownName(ctx) }, "알 수 없음"},
	}
}

func TestCalendarStrings_NilStoreFallbackByteEqual(t *testing.T) {
	t.Parallel()

	m := newCalendarMetrics(1)
	for _, c := range calendarStringCases() {
		if got := c.got(t.Context(), &m); got != c.want {
			t.Errorf("%s nil-store = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCalendarStrings_SeededStoreByteEqual(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	m := newCalendarMetrics(1)
	m.strings = store

	for _, c := range calendarStringCases() {
		if got := c.got(t.Context(), &m); got != c.want {
			t.Errorf("%s seeded-store = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCalendarStrings_SeededRowsMatchFallbackLiterals(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	cases := []struct {
		key      string
		fallback string
	}{
		{"header_month", "%d년 %d월 기념일"},
		{"summary", "총 %d건 · 생일 %d · 데뷔주년 %d"},
		{"empty", "등록된 기념일이 없습니다."},
		{"day", "%d월 %d일"},
		{"badge_birthday", "생일"},
		{"badge_anniversary", "데뷔 %d주년"},
		{"unknown", "알 수 없음"},
	}
	for _, c := range cases {
		got := store.GetContext(t.Context(), messagestrings.NamespaceCalendar, c.key)
		if got != c.fallback {
			t.Errorf("seeded calendar/%s = %q, want %q (must match code fallback byte-for-byte)", c.key, got, c.fallback)
		}
	}
}

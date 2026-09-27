package render

import (
	"context"
	"log/slog"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func loadSeededCalendarStore(t *testing.T) *messagestrings.Store {
	t.Helper()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	return store
}

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

// 코드 대체 문구는 없다. 문구 store가 없으면 문구는 비어 있고, 운영에서는 bot plane 기동 검증이 calendar key를
// 보장한다(DEC-20260926-hololive-message-strings-startup-validation).
func TestCalendarStrings_NoCodeFallbackWithoutStore(t *testing.T) {
	t.Parallel()

	m := newCalendarMetrics(1)

	if got := m.emptyText(t.Context()); got != "" {
		t.Fatalf("emptyText without store = %q, want empty (no code fallback)", got)
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

func TestCalendarStrings_SeededRowsMatchExpectedText(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.Default())
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message_strings: %v", err)
	}

	cases := []struct {
		key  messagestrings.Key
		want string
	}{
		{messagestrings.CalendarHeaderMonth, "%d년 %d월 기념일"},
		{messagestrings.CalendarSummary, "총 %d건 · 생일 %d · 데뷔주년 %d"},
		{messagestrings.CalendarEmpty, "등록된 기념일이 없습니다."},
		{messagestrings.CalendarDay, "%d월 %d일"},
		{messagestrings.CalendarBadgeBirthday, "생일"},
		{messagestrings.CalendarBadgeAnniversary, "데뷔 %d주년"},
		{messagestrings.CalendarUnknown, "알 수 없음"},
	}
	for _, c := range cases {
		if got := store.Text(c.key); got != c.want {
			t.Errorf("seeded %s = %q, want %q", c.key, got, c.want)
		}
	}
}

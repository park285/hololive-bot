package formatter

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (f *ResponseFormatter) CelebrationCalendar(_ context.Context, month, year int, entries []domain.CalendarEntry) string {
	if len(entries) == 0 {
		return fmt.Sprintf("📅 %d년 %d월 기념일\n\n등록된 기념일이 없습니다.", year, month)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📅 %d년 %d월 기념일 (%d건)\n", year, month, len(entries))
	b.WriteString("━━━━━━━━━━━━━━━━\n")

	currentDay := 0
	for _, e := range entries {
		if e.Day != currentDay {
			if currentDay > 0 {
				b.WriteString("\n")
			}
			currentDay = e.Day
			fmt.Fprintf(&b, "\n📌 %d월 %d일\n", month, e.Day)
		}
		formatCalendarEntry(&b, e)
	}

	return b.String()
}

func formatCalendarEntry(b *strings.Builder, e domain.CalendarEntry) {
	name := calendarMemberDisplayName(e.Member)
	switch e.Kind {
	case domain.CelebrationKindBirthday:
		fmt.Fprintf(b, "  🎂 %s 생일\n", name)
	case domain.CelebrationKindAnniversary:
		fmt.Fprintf(b, "  🎉 %s 데뷔 %d주년\n", name, e.Ordinal)
	}
}

func calendarMemberDisplayName(m *domain.Member) string {
	if m == nil {
		return "알 수 없음"
	}
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}
	if m.NameKo != "" {
		return m.NameKo
	}
	return m.Name
}

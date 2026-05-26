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
	b.WriteString(fmt.Sprintf("📅 %d년 %d월 기념일 (%d건)\n", year, month, len(entries)))
	b.WriteString("━━━━━━━━━━━━━━━━\n")

	currentDay := 0
	for _, e := range entries {
		if e.Day != currentDay {
			if currentDay > 0 {
				b.WriteString("\n")
			}
			currentDay = e.Day
			b.WriteString(fmt.Sprintf("\n📌 %d월 %d일\n", month, e.Day))
		}

		name := calendarMemberDisplayName(e.Member)
		switch e.Kind {
		case domain.CelebrationKindBirthday:
			b.WriteString(fmt.Sprintf("  🎂 %s 생일\n", name))
		case domain.CelebrationKindAnniversary:
			b.WriteString(fmt.Sprintf("  🎉 %s 데뷔 %d주년\n", name, e.Ordinal))
		}
	}

	return b.String()
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

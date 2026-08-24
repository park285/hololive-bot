package render

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type dayGroup struct {
	day     int
	entries []domain.CalendarEntry
}

func groupEntriesByDay(entries []domain.CalendarEntry) []dayGroup {
	var (
		groups  []dayGroup
		current *dayGroup
	)

	for _, e := range entries {
		if current == nil || current.day != e.Day {
			if current != nil {
				groups = append(groups, *current)
			}

			current = &dayGroup{day: e.Day}
		}

		current.entries = append(current.entries, e)
	}

	if current != nil {
		groups = append(groups, *current)
	}

	return groups
}

func calculateCanvasHeight(m *calendarMetrics, groups []dayGroup) int {
	h := m.headerH + separatorH + m.paddingY

	if len(groups) == 0 {
		return h + int(60*m.sf) + m.paddingY
	}

	for _, g := range groups {
		h += m.dateHeaderH + len(g.entries)*m.entryRowH + m.dateSectGap
	}

	return h + m.paddingY
}

func entryDisplayName(ctx context.Context, m *calendarMetrics, member *domain.Member) string {
	if member == nil {
		return m.unknownName(ctx)
	}

	if member.ShortKoreanName != "" {
		return member.ShortKoreanName
	}

	if member.NameKo != "" {
		return member.NameKo
	}

	return member.Name
}

func (m *calendarMetrics) calStr(ctx context.Context, key, fallback string) string {
	return m.strings.GetOrContext(ctx, messagestrings.NamespaceCalendar, key, fallback)
}

func (m *calendarMetrics) headerText(ctx context.Context, year, month int) string {
	return fmt.Sprintf(m.calStr(ctx, "header_month", "%d년 %d월 기념일"), year, month)
}

func (m *calendarMetrics) summaryText(ctx context.Context, total, birthday, anniversary int) string {
	return fmt.Sprintf(m.calStr(ctx, "summary", "총 %d건 · 생일 %d · 데뷔주년 %d"), total, birthday, anniversary)
}

func (m *calendarMetrics) emptyText(ctx context.Context) string {
	return m.calStr(ctx, "empty", "등록된 기념일이 없습니다.")
}

func (m *calendarMetrics) dayText(ctx context.Context, month, day int) string {
	return fmt.Sprintf(m.calStr(ctx, "day", "%d월 %d일"), month, day)
}

func (m *calendarMetrics) badgeBirthday(ctx context.Context) string {
	return m.calStr(ctx, "badge_birthday", "생일")
}

func (m *calendarMetrics) anniversaryBadge(ctx context.Context, ordinal int) string {
	return fmt.Sprintf(m.calStr(ctx, "badge_anniversary", "데뷔 %d주년"), ordinal)
}

func (m *calendarMetrics) unknownName(ctx context.Context) string {
	return m.calStr(ctx, "unknown", "알 수 없음")
}

package formatter

import (
	"context"

	"github.com/kapu/hololive-api/internal/templateview"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type calendarEntryView struct {
	Name       string
	IsBirthday bool
	Years      int
}

type calendarDayView struct {
	Month   int
	Day     int
	Entries []calendarEntryView
}

type calendarTemplateData struct {
	Year  int
	Month int
	Count int
	Days  []calendarDayView
}

func (f *ResponseFormatter) CelebrationCalendar(ctx context.Context, month, year int, entries []domain.CalendarEntry) string {
	days := calendarDayViews(month, entries)
	data := calendarTemplateData{
		Year:  year,
		Month: month,
		Count: calendarEntryCount(days),
		Days:  days,
	}

	return f.renderResponse(ctx, domain.TemplateKeyCmdCalendar, data, templateview.ShouldFoldItems(data.Count))
}

func calendarDayViews(month int, entries []domain.CalendarEntry) []calendarDayView {
	var days []calendarDayView

	currentDay := 0

	for _, e := range entries {
		if e.Member == nil {
			continue
		}

		if len(days) == 0 || e.Day != currentDay {
			currentDay = e.Day
			days = append(days, calendarDayView{Month: month, Day: e.Day})
		}

		last := len(days) - 1

		days[last].Entries = append(days[last].Entries, calendarEntryView{
			Name:       e.Member.DisplayName(),
			IsBirthday: e.Kind == domain.CelebrationKindBirthday,
			Years:      e.Ordinal,
		})
	}

	return days
}

func calendarEntryCount(days []calendarDayView) int {
	count := 0

	for _, d := range days {
		count += len(d.Entries)
	}

	return count
}

// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package formatter

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
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

	rendered, err := f.render(ctx, domain.TemplateKeyCmdCalendar, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
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
			Name:       calendarMemberDisplayName(e.Member),
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

func calendarMemberDisplayName(m *domain.Member) string {
	if m == nil {
		return ""
	}
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}
	if m.NameKo != "" {
		return m.NameKo
	}
	return m.Name
}

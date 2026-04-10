package templateview

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type MajorEventView struct {
	Title    string
	DateStr  string
	Members  string
	Link     string
	HasDates bool
}

func BuildMajorEventViews(events []domain.MajorEvent) []MajorEventView {
	views := make([]MajorEventView, 0, len(events))
	for i := range events {
		event := &events[i]
		views = append(views, MajorEventView{
			Title:    event.Title,
			DateStr:  FormatMajorEventDatesFromDB(event.EventStartDate, event.EventEndDate),
			Members:  strings.Join(event.Members, ", "),
			Link:     event.Link,
			HasDates: event.EventStartDate != nil,
		})
	}
	return views
}

func FormatMajorEventDatesFromDB(start, end *time.Time) string {
	if start == nil {
		return "TBA"
	}

	weekdays := []string{"일", "월", "화", "수", "목", "금", "토"}
	formatDate := func(t time.Time) string {
		return fmt.Sprintf("%d년 %d월 %d일(%s)", t.Year(), t.Month(), t.Day(), weekdays[t.Weekday()])
	}

	if end == nil || start.Equal(*end) {
		return formatDate(*start)
	}

	return fmt.Sprintf("%s ~ %s", formatDate(*start), formatDate(*end))
}

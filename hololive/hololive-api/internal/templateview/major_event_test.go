package templateview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildMajorEventViewsAndDateFormatting(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)

	events := []domain.MajorEvent{
		{
			Title:          "Range Event",
			EventStartDate: &start,
			EventEndDate:   &end,
			Members:        []string{"A", "B"},
			Link:           "https://example.com/range",
		},
		{
			Title:   "TBA Event",
			Members: []string{"C"},
			Link:    "https://example.com/tba",
		},
	}

	views := BuildMajorEventViews(events)
	require.Len(t, views, 2)

	assert.Equal(t, "Range Event", views[0].Title)
	assert.Contains(t, views[0].DateStr, "~")
	assert.True(t, views[0].HasDates)
	assert.Equal(t, "A, B", views[0].Members)

	assert.Equal(t, "TBA", views[1].DateStr)
	assert.False(t, views[1].HasDates)
}

func TestFormatMajorEventDatesFromDB(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.March, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, "TBA", FormatMajorEventDatesFromDB(nil, nil))
	assert.Contains(t, FormatMajorEventDatesFromDB(&start, nil), "2026년 3월 6일")
	assert.Contains(t, FormatMajorEventDatesFromDB(&start, &start), "2026년 3월 6일")
	assert.Contains(t, FormatMajorEventDatesFromDB(&start, &end), "~")
}

func TestBuildMajorEventViewsPreservesOnlySafeLinks(t *testing.T) {
	for _, link := range []string{"http://example.com", "javascript:alert(1)", "https://user@example.com", "https://example.com/a)b", "https://example.com/\n"} {
		views := BuildMajorEventViews([]domain.MajorEvent{{Link: link}})
		if link == "https://example.com/\n" {
			assert.Equal(t, "https://example.com/", views[0].Link)
		} else {
			assert.Empty(t, views[0].Link)
		}
	}

	views := BuildMajorEventViews([]domain.MajorEvent{{Link: "https://example.com/event"}})
	assert.Equal(t, "https://example.com/event", views[0].Link)
}

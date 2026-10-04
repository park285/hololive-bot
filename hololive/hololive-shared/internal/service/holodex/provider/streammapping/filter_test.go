package streammapping

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFilterUpcomingStreamsInWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 0, 0, 0, 0, time.UTC)
	until := now.Add(24 * time.Hour)

	for _, tc := range []struct {
		name      string
		scheduled *time.Time
		actual    *time.Time
		want      bool
	}{
		{name: "before upper bound", scheduled: new(until.Add(-time.Nanosecond)), want: true},
		{name: "at upper bound", scheduled: &until, want: true},
		{name: "after upper bound", scheduled: new(until.Add(time.Nanosecond))},
		{name: "same instant in UTC offset", scheduled: new(until.In(time.FixedZone("JST", 9*60*60))), want: true},
		{name: "offset after upper bound", scheduled: new(until.Add(time.Nanosecond).In(time.FixedZone("JST", 9*60*60)))},
		{name: "at lower bound", scheduled: &now},
		{name: "delayed", scheduled: new(now.Add(-time.Hour))},
		{name: "already started", scheduled: new(now.Add(time.Hour)), actual: &now},
		{name: "unknown schedule", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stream := &domain.Stream{ID: tc.name, StartScheduled: tc.scheduled, StartActual: tc.actual}
			got := NewStreamFilter(nil).FilterUpcomingStreamsInWindow([]*domain.Stream{stream}, now, until)

			if (len(got) == 1) != tc.want {
				t.Fatalf("kept stream = %t, want %t", len(got) == 1, tc.want)
			}
		})
	}
}

func TestFilterUpcomingStreamsInWindowWithoutUpperBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 0, 0, 0, 0, time.UTC)
	stream := &domain.Stream{ID: "distant", StartScheduled: new(now.Add(400 * time.Hour))}
	got := NewStreamFilter(nil).FilterUpcomingStreamsInWindow([]*domain.Stream{stream}, now, time.Time{})

	if len(got) != 1 || got[0] != stream {
		t.Fatalf("streams = %v, want distant stream with no upper bound", got)
	}
}

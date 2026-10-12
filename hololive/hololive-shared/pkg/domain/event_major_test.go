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

package domain

import (
	"testing"
	"time"
)

func TestMajorEvent_HasEventDates(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name           string
		eventStartDate *time.Time
		eventDates     []time.Time
		expected       bool
	}{
		{
			name:           "nil start date and empty dates",
			eventStartDate: nil,
			eventDates:     nil,
			expected:       false,
		},
		{
			name:           "nil start date and empty slice",
			eventStartDate: nil,
			eventDates:     []time.Time{},
			expected:       false,
		},
		{
			name:           "has start date",
			eventStartDate: &now,
			eventDates:     nil,
			expected:       true,
		},
		{
			name:           "has event dates slice",
			eventStartDate: nil,
			eventDates:     []time.Time{now},
			expected:       true,
		},
		{
			name:           "both set",
			eventStartDate: &now,
			eventDates:     []time.Time{now, now.AddDate(0, 0, 1)},
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &MajorEvent{
				EventStartDate: tt.eventStartDate,
				EventDates:     tt.eventDates,
			}
			if got := event.HasEventDates(); got != tt.expected {
				t.Errorf("HasEventDates() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMajorEvent_SetEventDatesFromParsed(t *testing.T) {
	now := time.Now()
	tomorrow := now.AddDate(0, 0, 1)
	dayAfter := now.AddDate(0, 0, 2)

	tests := []struct {
		name          string
		eventDates    []time.Time
		expectedStart *time.Time
		expectedEnd   *time.Time
	}{
		{
			name:          "empty dates",
			eventDates:    []time.Time{},
			expectedStart: nil,
			expectedEnd:   nil,
		},
		{
			name:          "single date",
			eventDates:    []time.Time{now},
			expectedStart: &now,
			expectedEnd:   &now,
		},
		{
			name:          "multiple dates",
			eventDates:    []time.Time{now, tomorrow, dayAfter},
			expectedStart: &now,
			expectedEnd:   &dayAfter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &MajorEvent{EventDates: tt.eventDates}
			event.SetEventDatesFromParsed()

			assertMajorEventDate(t, "start", event.EventStartDate, tt.expectedStart)
			assertMajorEventDate(t, "end", event.EventEndDate, tt.expectedEnd)
		})
	}
}

func assertMajorEventDate(t *testing.T, name string, got, want *time.Time) {
	t.Helper()

	switch {
	case want == nil && got != nil:
		t.Errorf("expected nil %s date, got %v", name, got)
	case want != nil && got == nil:
		t.Errorf("expected %s %v, got nil", name, want)
	case want != nil && !got.Equal(*want):
		t.Errorf("expected %s %v, got %v", name, want, got)
	}
}

func TestMajorEvent_IsNotified(t *testing.T) {
	event := &MajorEvent{NotifiedWeek: "2026-04"}

	if !event.IsNotified("2026-04") {
		t.Error("expected IsNotified to return true for matching week")
	}

	if event.IsNotified("2026-05") {
		t.Error("expected IsNotified to return false for non-matching week")
	}
}

func TestMajorEvent_MarkAsNotified(t *testing.T) {
	event := &MajorEvent{}
	now := time.Now()

	event.MarkAsNotified("2026-04", now)

	if event.NotifiedWeek != "2026-04" {
		t.Errorf("expected NotifiedWeek %q, got %q", "2026-04", event.NotifiedWeek)
	}

	if event.NotifiedAt == nil || !event.NotifiedAt.Equal(now) {
		t.Errorf("expected NotifiedAt %v, got %v", now, event.NotifiedAt)
	}
}

func TestMajorEvent_TableName(t *testing.T) {
	event := MajorEvent{}
	expected := "major_events"

	if got := event.TableName(); got != expected {
		t.Errorf("expected table name %q, got %q", expected, got)
	}
}

func TestEventRoomSubscription_TableName(t *testing.T) {
	sub := EventRoomSubscription{}
	expected := "major_event_subscriptions"

	if got := sub.TableName(); got != expected {
		t.Errorf("expected table name %q, got %q", expected, got)
	}
}

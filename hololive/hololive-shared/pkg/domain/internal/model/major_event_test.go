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

package model

import (
	"testing"
	"time"
)

func TestNewMajorEvent(t *testing.T) {
	title := "hololive SUPER EXPO 2026"
	link := "https://example.com/event"
	pubDate := time.Now()

	event := NewMajorEvent(title, link, pubDate)

	if event.Title != title {
		t.Errorf("expected title %q, got %q", title, event.Title)
	}
	if event.Link != link {
		t.Errorf("expected link %q, got %q", link, event.Link)
	}
	if event.ExternalID != link {
		t.Errorf("expected externalID %q, got %q", link, event.ExternalID)
	}
	if event.PubDate == nil || !event.PubDate.Equal(pubDate) {
		t.Errorf("expected pubDate %v, got %v", pubDate, event.PubDate)
	}
	if event.Status != MajorEventStatusActive {
		t.Errorf("expected status %q, got %q", MajorEventStatusActive, event.Status)
	}
	if event.Type != MajorEventTypeEvent {
		t.Errorf("expected type %q, got %q", MajorEventTypeEvent, event.Type)
	}
	if event.LinkStatus != MajorEventLinkStatusUnchecked {
		t.Errorf("expected link status %q, got %q", MajorEventLinkStatusUnchecked, event.LinkStatus)
	}
}

func TestNewMajorNews(t *testing.T) {
	title := "ホロライブ新商品発売"
	link := "https://example.com/news"
	pubDate := time.Now()

	news := NewMajorNews(title, link, pubDate)

	if news.Title != title {
		t.Errorf("expected title %q, got %q", title, news.Title)
	}
	if news.Link != link {
		t.Errorf("expected link %q, got %q", link, news.Link)
	}
	if news.ExternalID != link {
		t.Errorf("expected externalID %q, got %q", link, news.ExternalID)
	}
	if news.PubDate == nil || !news.PubDate.Equal(pubDate) {
		t.Errorf("expected pubDate %v, got %v", pubDate, news.PubDate)
	}
	if news.Status != MajorEventStatusActive {
		t.Errorf("expected status %q, got %q", MajorEventStatusActive, news.Status)
	}
	if news.Type != MajorEventTypeNews {
		t.Errorf("expected type %q, got %q", MajorEventTypeNews, news.Type)
	}
	if news.LinkStatus != MajorEventLinkStatusUnchecked {
		t.Errorf("expected link status %q, got %q", MajorEventLinkStatusUnchecked, news.LinkStatus)
	}
}

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

			if tt.expectedStart == nil {
				if event.EventStartDate != nil {
					t.Errorf("expected nil start date, got %v", event.EventStartDate)
				}
			} else {
				if event.EventStartDate == nil || !event.EventStartDate.Equal(*tt.expectedStart) {
					t.Errorf("expected start %v, got %v", tt.expectedStart, event.EventStartDate)
				}
			}

			if tt.expectedEnd == nil {
				if event.EventEndDate != nil {
					t.Errorf("expected nil end date, got %v", event.EventEndDate)
				}
			} else {
				if event.EventEndDate == nil || !event.EventEndDate.Equal(*tt.expectedEnd) {
					t.Errorf("expected end %v, got %v", tt.expectedEnd, event.EventEndDate)
				}
			}
		})
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

func TestNewEventRoomSubscription(t *testing.T) {
	roomID := "room123"
	roomName := "테스트방"

	sub := NewEventRoomSubscription(roomID, roomName)

	if sub.RoomID != roomID {
		t.Errorf("expected roomID %q, got %q", roomID, sub.RoomID)
	}
	if sub.RoomName != roomName {
		t.Errorf("expected roomName %q, got %q", roomName, sub.RoomName)
	}
	if sub.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestEventRoomSubscription_TableName(t *testing.T) {
	sub := EventRoomSubscription{}
	expected := "major_event_subscriptions"
	if got := sub.TableName(); got != expected {
		t.Errorf("expected table name %q, got %q", expected, got)
	}
}

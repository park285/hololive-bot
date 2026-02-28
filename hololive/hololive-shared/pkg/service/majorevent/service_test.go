package majorevent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type mockHTTPClient struct {
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}

func TestNewService(t *testing.T) {
	client := &mockHTTPClient{}
	svc := NewService(client)

	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.rssURL != defaultRSSURL {
		t.Errorf("rssURL = %q, want %q", svc.rssURL, defaultRSSURL)
	}
}

func TestNewService_WithOptions(t *testing.T) {
	client := &mockHTTPClient{}
	customURL := "https://example.com/feed"

	svc := NewService(client, WithRSSURL(customURL))

	if svc.rssURL != customURL {
		t.Errorf("rssURL = %q, want %q", svc.rssURL, customURL)
	}
}

func TestService_FetchEvents(t *testing.T) {
	rssXML := `<?xml version="1.0"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
<item>
  <title>Test Event</title>
  <link>https://example.com/event</link>
  <pubDate>Thu, 09 Jan 2025 05:00:00 +0000</pubDate>
  <category>Member1</category>
  <content:encoded><![CDATA[<p>2026年3月6日</p>]]></content:encoded>
</item>
</channel>
</rss>`

	client := &mockHTTPClient{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(rssXML)),
		},
	}

	svc := NewService(client)
	events, err := svc.FetchEvents(context.Background())

	if err != nil {
		t.Fatalf("FetchEvents() error = %v", err)
	}

	if len(events) != 1 {
		t.Errorf("got %d events, want 1", len(events))
	}

	if events[0].Title != "Test Event" {
		t.Errorf("title = %q, want %q", events[0].Title, "Test Event")
	}

	if len(events[0].EventDates) == 0 {
		t.Error("EventDates should not be empty after date extraction")
	}

	if events[0].Description == "" {
		t.Error("Description should not be empty - namespace parsing failed")
	}
}

func TestService_FetchEvents_HTTPError(t *testing.T) {
	client := &mockHTTPClient{
		err: errors.New("connection refused"),
	}

	svc := NewService(client)
	_, err := svc.FetchEvents(context.Background())

	if err == nil {
		t.Error("expected error for HTTP failure")
	}
}

func TestService_FetchEvents_BadStatus(t *testing.T) {
	client := &mockHTTPClient{
		response: &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader("")),
		},
	}

	svc := NewService(client)
	_, err := svc.FetchEvents(context.Background())

	if err == nil {
		t.Error("expected error for 500 status")
	}
}

func TestService_FilterWeeklyEvents(t *testing.T) {
	svc := NewService(&mockHTTPClient{})

	weekStart := time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)
	weekEnd := time.Date(2026, 3, 13, 23, 59, 59, 0, time.UTC)

	events := []domain.MajorEvent{
		{
			Title:      "In Range",
			EventDates: []time.Time{time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)},
		},
		{
			Title:      "Before Range",
			EventDates: []time.Time{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		},
		{
			Title:      "After Range",
			EventDates: []time.Time{time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)},
		},
		{
			Title:      "No Dates",
			EventDates: nil,
		},
		{
			Title:      "At Start",
			EventDates: []time.Time{time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC)},
		},
		{
			Title:      "At End",
			EventDates: []time.Time{time.Date(2026, 3, 13, 0, 0, 0, 0, time.UTC)},
		},
	}

	filtered := svc.FilterWeeklyEvents(events, weekStart, weekEnd)

	if len(filtered) != 3 {
		t.Errorf("got %d filtered events, want 3", len(filtered))
	}

	expectedTitles := map[string]bool{
		"In Range": true,
		"At Start": true,
		"At End":   true,
	}

	for _, e := range filtered {
		if !expectedTitles[e.Title] {
			t.Errorf("unexpected event in filtered: %q", e.Title)
		}
	}
}

func TestGetWeekRange(t *testing.T) {
	kst := time.FixedZone("KST", 9*60*60)

	tests := []struct {
		name             string
		now              time.Time
		expectedStartDay int
		expectedEndDay   int
	}{
		{
			name:             "Monday 09:00 → same week Mon~Sun",
			now:              time.Date(2026, 1, 19, 9, 0, 0, 0, kst),
			expectedStartDay: 19,
			expectedEndDay:   25,
		},
		{
			name:             "Wednesday midday → same week Mon~Sun",
			now:              time.Date(2026, 1, 21, 12, 0, 0, 0, kst),
			expectedStartDay: 19,
			expectedEndDay:   25,
		},
		{
			name:             "Sunday evening → same week Mon~Sun",
			now:              time.Date(2026, 1, 25, 20, 0, 0, 0, kst),
			expectedStartDay: 19,
			expectedEndDay:   25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := GetWeekRange(tt.now)

			if start.Weekday() != time.Monday {
				t.Errorf("start weekday = %v, want Monday", start.Weekday())
			}
			if end.Weekday() != time.Sunday {
				t.Errorf("end weekday = %v, want Sunday", end.Weekday())
			}
			if start.Day() != tt.expectedStartDay {
				t.Errorf("start day = %d, want %d", start.Day(), tt.expectedStartDay)
			}
			if end.Day() != tt.expectedEndDay {
				t.Errorf("end day = %d, want %d", end.Day(), tt.expectedEndDay)
			}
		})
	}
}

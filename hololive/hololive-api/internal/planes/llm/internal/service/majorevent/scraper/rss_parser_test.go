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

package scraper

import (
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>hololive events</title>
    <item>
      <title>hololive SUPER EXPO 2026</title>
      <link>https://hololive.hololivepro.com/events/superexpo2026/</link>
      <guid>https://hololive.hololivepro.com/events/superexpo2026/</guid>
      <pubDate>Thu, 09 Jan 2025 05:00:00 +0000</pubDate>
      <category>ときのそら</category>
      <category>星街すいせい</category>
      <content:encoded><![CDATA[event details]]></content:encoded>
    </item>
  </channel>
</rss>`

func TestRSSParserParse(t *testing.T) {
	t.Parallel()

	parser := NewRSSParser()
	events, err := parser.Parse([]byte(sampleRSS), domain.MajorEventTypeEvent)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Parse() len = %d, want 1", len(events))
	}

	got := events[0]
	if got.Type != domain.MajorEventTypeEvent {
		t.Fatalf("event type = %s, want %s", got.Type, domain.MajorEventTypeEvent)
	}
	if got.Title != "hololive SUPER EXPO 2026" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.ExternalID == "" || got.Link == "" {
		t.Fatalf("external id/link should not be empty")
	}
	if len(got.Members) != 2 {
		t.Fatalf("members len = %d, want 2", len(got.Members))
	}
	if got.PubDate == nil {
		t.Fatalf("pub date should be parsed")
	}
}

func TestParseRSSDate(t *testing.T) {
	t.Parallel()

	if _, ok := parseRSSDate("Fri, 12 Dec 2025 02:50:11 +0000"); !ok {
		t.Fatal("expected RFC1123Z date to parse")
	}
	if _, ok := parseRSSDate("Fri, 12 Dec 2025 02:50:11 GMT"); !ok {
		t.Fatal("expected RFC1123 date to parse")
	}
	if _, ok := parseRSSDate("2025-01-09"); ok {
		t.Fatal("unexpected parse success for invalid date format")
	}
}

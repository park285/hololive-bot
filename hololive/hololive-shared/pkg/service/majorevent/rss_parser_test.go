package majorevent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRSSParser_Parse(t *testing.T) {
	parser := NewRSSParser()

	data, err := os.ReadFile(filepath.Join("testdata", "events_feed.xml"))
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	events, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	t.Run("first event", func(t *testing.T) {
		event := events[0]
		if event.Title != "hololive SUPER EXPO 2026" {
			t.Errorf("title = %q, want %q", event.Title, "hololive SUPER EXPO 2026")
		}
		if event.Link != "https://hololive.hololivepro.com/events/superexpo2026/" {
			t.Errorf("link = %q", event.Link)
		}
		if len(event.Members) != 2 {
			t.Errorf("members count = %d, want 2", len(event.Members))
		}
		if event.Members[0] != "ときのそら" {
			t.Errorf("first member = %q, want ときのそら", event.Members[0])
		}
		if event.Type != domain.MajorEventTypeEvent {
			t.Errorf("type = %q, want %q", event.Type, domain.MajorEventTypeEvent)
		}
	})

	t.Run("second event", func(t *testing.T) {
		event := events[1]
		if event.Title != "7th fes. -Flame of Paradox-" {
			t.Errorf("title = %q", event.Title)
		}
		if len(event.Members) != 1 {
			t.Errorf("members count = %d, want 1", len(event.Members))
		}
	})
}

func TestRSSParser_Parse_EmptyFeed(t *testing.T) {
	parser := NewRSSParser()

	data := []byte(`<?xml version="1.0"?><rss><channel><title>Empty</title></channel></rss>`)
	events, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestRSSParser_Parse_InvalidXML(t *testing.T) {
	parser := NewRSSParser()

	data := []byte(`not valid xml`)
	_, err := parser.Parse(data)
	if err == nil {
		t.Error("expected error for invalid XML")
	}
}

func TestRSSParser_parsePubDate(t *testing.T) {
	parser := NewRSSParser()

	tests := []struct {
		name     string
		input    string
		wantYear int
		wantErr  bool
	}{
		{
			name:     "RFC1123Z format",
			input:    "Thu, 09 Jan 2025 05:00:00 +0000",
			wantYear: 2025,
		},
		{
			name:     "RFC1123 format with timezone",
			input:    "Fri, 12 Dec 2025 02:50:11 +0000",
			wantYear: 2025,
		},
		{
			name:    "invalid format",
			input:   "2025-01-09",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.parsePubDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePubDate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got.Year() != tt.wantYear {
				t.Errorf("year = %d, want %d", got.Year(), tt.wantYear)
			}
		})
	}
}

func TestRSSParser_ParseWithType(t *testing.T) {
	parser := NewRSSParser()

	data := []byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
<title>NEWS</title>
<item>
<title>ホロライブ新商品発売</title>
<link>https://example.com/news/1</link>
<pubDate>Thu, 09 Jan 2025 05:00:00 +0000</pubDate>
</item>
</channel>
</rss>`)

	events, err := parser.ParseWithType(data, domain.MajorEventTypeNews)
	if err != nil {
		t.Fatalf("ParseWithType() error = %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Type != domain.MajorEventTypeNews {
		t.Errorf("type = %q, want %q", events[0].Type, domain.MajorEventTypeNews)
	}
}

func TestRSSParser_Parse_WithDescription(t *testing.T) {
	parser := NewRSSParser()

	data, err := os.ReadFile(filepath.Join("testdata", "events_feed.xml"))
	if err != nil {
		t.Fatalf("failed to read test fixture: %v", err)
	}

	events, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// content:encoded 네임스페이스 파싱이 반드시 성공해야 함
	if events[0].Description == "" {
		t.Fatal("Description should not be empty - content:encoded namespace parsing failed")
	}

	// 실제 내용이 파싱되었는지 확인
	if !strings.Contains(events[0].Description, "2026年3月6日") {
		t.Errorf("Description should contain date, got: %q", events[0].Description)
	}

	// 두 번째 이벤트도 확인
	if events[1].Description == "" {
		t.Error("Second event description should not be empty")
	}
}

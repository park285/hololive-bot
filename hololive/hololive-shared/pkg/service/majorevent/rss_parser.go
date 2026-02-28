package majorevent

import (
	"encoding/xml"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// RSSFeed: RSS 피드 루트 구조체
type RSSFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Channel RSSChannel `xml:"channel"`
}

// RSSChannel: RSS 채널 구조체
type RSSChannel struct {
	Title string    `xml:"title"`
	Items []RSSItem `xml:"item"`
}

// RSSItem: RSS 아이템 구조체
type RSSItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	PubDate     string   `xml:"pubDate"`
	Categories  []string `xml:"category"`
	Description string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

// RSSParser: RSS Feed를 파싱하는 파서
type RSSParser struct{}

// NewRSSParser: RSSParser 인스턴스를 생성합니다.
func NewRSSParser() *RSSParser {
	return &RSSParser{}
}

// Parse: RSS XML 데이터를 파싱하여 MajorEvent 목록을 반환합니다.
func (p *RSSParser) Parse(data []byte) ([]domain.MajorEvent, error) {
	return p.ParseWithType(data, domain.MajorEventTypeEvent)
}

// ParseWithType: RSS XML 데이터를 지정된 타입으로 파싱합니다.
func (p *RSSParser) ParseWithType(data []byte, eventType domain.MajorEventType) ([]domain.MajorEvent, error) {
	var feed RSSFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("parse RSS XML: %w", err)
	}

	events := make([]domain.MajorEvent, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		var pubDatePtr *time.Time
		if pubDate, err := p.parsePubDate(item.PubDate); err == nil {
			pubDatePtr = &pubDate
		}

		event := domain.MajorEvent{
			Title:       item.Title,
			Link:        item.Link,
			ExternalID:  item.Link,
			PubDate:     pubDatePtr,
			Members:     item.Categories,
			Description: item.Description,
			Type:        eventType,
			Status:      domain.MajorEventStatusActive,
		}
		events = append(events, event)
	}

	return events, nil
}

// parsePubDate: RFC1123 형식의 pubDate를 파싱합니다.
func (p *RSSParser) parsePubDate(dateStr string) (time.Time, error) {
	formats := []string{
		time.RFC1123Z,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 -0700",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("parse pubDate %q: unknown format", dateStr)
}

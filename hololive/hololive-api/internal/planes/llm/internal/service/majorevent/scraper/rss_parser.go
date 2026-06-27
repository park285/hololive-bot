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
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// RSSParser는 RSS/Atom 피드를 MajorEvent 목록으로 변환한다.
type RSSParser struct {
	parser *gofeed.Parser
}

// NewRSSParser는 RSSParser를 생성한다.
func NewRSSParser() *RSSParser {
	return &RSSParser{
		parser: gofeed.NewParser(),
	}
}

// Parse는 raw feed 바이트를 MajorEvent 목록으로 파싱한다.
func (p *RSSParser) Parse(data []byte, eventType domain.MajorEventType) ([]*domain.MajorEvent, error) {
	if p == nil || p.parser == nil {
		return nil, fmt.Errorf("parse rss: parser is nil")
	}

	feed, err := p.parser.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse rss: parse feed: %w", err)
	}

	events := make([]*domain.MajorEvent, 0, len(feed.Items))
	for _, item := range feed.Items {
		if item == nil {
			continue
		}

		event, ok := mapFeedItemToEvent(item, eventType)
		if !ok {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

func mapFeedItemToEvent(item *gofeed.Item, eventType domain.MajorEventType) (*domain.MajorEvent, bool) {
	link := strings.TrimSpace(item.Link)
	externalID := strings.TrimSpace(item.GUID)
	if externalID == "" {
		externalID = link
	}
	if externalID == "" {
		return nil, false
	}
	if link == "" {
		link = externalID
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "(untitled)"
	}

	description := strings.TrimSpace(item.Content)
	if description == "" {
		description = strings.TrimSpace(item.Description)
	}

	pubDate := resolvePublishedAt(item)

	event := &domain.MajorEvent{
		ExternalID:  externalID,
		Type:        eventType,
		Title:       title,
		Link:        link,
		Description: description,
		Members:     normalizeMembers(item.Categories),
		Status:      domain.MajorEventStatusActive,
		LinkStatus:  domain.MajorEventLinkStatusUnchecked,
	}
	if pubDate != nil {
		pubDateUTC := pubDate.UTC()
		event.PubDate = &pubDateUTC
	}

	return event, true
}

func normalizeMembers(categories []string) []string {
	if len(categories) == 0 {
		return nil
	}

	members := make([]string, 0, len(categories))
	for _, category := range categories {
		trimmed := strings.TrimSpace(category)
		if trimmed == "" {
			continue
		}
		members = append(members, trimmed)
	}
	return members
}

func resolvePublishedAt(item *gofeed.Item) *time.Time {
	if item == nil {
		return nil
	}
	if item.PublishedParsed != nil {
		published := item.PublishedParsed.UTC()
		return &published
	}
	if item.UpdatedParsed != nil {
		updated := item.UpdatedParsed.UTC()
		return &updated
	}
	if parsed, ok := parseRSSDate(item.Published); ok {
		return &parsed
	}
	if parsed, ok := parseRSSDate(item.Updated); ok {
		return &parsed
	}
	return nil
}

func parseRSSDate(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}

	if parsed, err := time.Parse(time.RFC1123Z, trimmed); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse(time.RFC1123, trimmed); err == nil {
		return parsed.UTC(), true
	}

	formats := []string{
		"Mon, 02 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 MST",
	}
	for _, layout := range formats {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

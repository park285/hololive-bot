package parser

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

type rssFeed struct {
	Entries []rssEntry `xml:"entry"`
}

type rssEntry struct {
	VideoID     string        `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
	Title       string        `xml:"title"`
	Published   string        `xml:"published"`
	AuthorName  string        `xml:"author>name"`
	MediaGroup  rssMediaGroup `xml:"http://search.yahoo.com/mrss/ group"`
	MediaThumbs []rssThumb    `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type rssMediaGroup struct {
	Thumbnails []rssThumb `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

type rssThumb struct {
	URL    string `xml:"url,attr"`
	Width  int    `xml:"width,attr"`
	Height int    `xml:"height,attr"`
}

func ParseVideosFromRSSFeed(feedXML, channelID string, maxResults int) ([]*Video, error) {
	if strings.TrimSpace(feedXML) == "" {
		return []*Video{}, nil
	}
	if maxResults <= 0 {
		return []*Video{}, nil
	}

	var feed rssFeed
	if err := xml.Unmarshal([]byte(feedXML), &feed); err != nil {
		return nil, fmt.Errorf("parse rss feed xml: %w", err)
	}

	videos := make([]*Video, 0, min(len(feed.Entries), maxResults))
	seen := make(map[string]struct{}, maxResults)

	for _, entry := range feed.Entries {
		if len(videos) >= maxResults {
			break
		}
		video, ok := parseVideoFromRSSEntry(entry, channelID, seen)
		if !ok {
			continue
		}
		videos = append(videos, video)
	}

	return videos, nil
}

func parseVideoFromRSSEntry(entry rssEntry, channelID string, seen map[string]struct{}) (*Video, bool) {
	videoID := strings.TrimSpace(entry.VideoID)
	title := strings.TrimSpace(entry.Title)
	if videoID == "" || title == "" {
		return nil, false
	}
	if _, exists := seen[videoID]; exists {
		return nil, false
	}
	seen[videoID] = struct{}{}

	return &Video{
		VideoID:       videoID,
		Title:         title,
		Thumbnail:     rssEntryThumbnails(entry),
		PublishedText: formatRSSPublishedText(entry.Published),
		ChannelID:     channelID,
		ChannelTitle:  strings.TrimSpace(entry.AuthorName),
	}, true
}

func formatRSSPublishedText(published string) string {
	publishedText := strings.TrimSpace(published)
	if parsed, err := time.Parse(time.RFC3339, publishedText); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return publishedText
}

func rssEntryThumbnails(entry rssEntry) []Thumbnail {
	thumbnails := entry.MediaGroup.Thumbnails
	if len(thumbnails) == 0 && len(entry.MediaThumbs) > 0 {
		thumbnails = entry.MediaThumbs
	}

	convertedThumbs := make([]Thumbnail, 0, len(thumbnails))
	for _, thumb := range thumbnails {
		if strings.TrimSpace(thumb.URL) == "" {
			continue
		}
		convertedThumbs = append(convertedThumbs, Thumbnail{
			URL:    thumb.URL,
			Width:  thumb.Width,
			Height: thumb.Height,
		})
	}
	return convertedThumbs
}

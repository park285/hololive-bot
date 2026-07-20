package parser

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	yttimestamp "github.com/kapu/hololive-shared/internal/service/youtube/timestamp"
)

var ErrPublishedAtNotFound = errors.New("published_at not found")
var ErrCommunityPublishedAtNotFound = errors.New("community published_at not found")

var communityPublishedAtJSONPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)"(?:datePublished|dateCreated|uploadDate|publishDate)"\s*:\s*"([^"]+)"`),
}

func NormalizePublishedAtCandidate(value string) (*time.Time, bool) {
	return yttimestamp.ParsePublishedAt(value)
}

func ExtractPublishedAtFromHTML(html string) (*time.Time, error) {
	for _, candidate := range collectPublishedAtCandidates(html) {
		publishedAt, ok := NormalizePublishedAtCandidate(candidate)
		if ok {
			return publishedAt, nil
		}
	}

	return nil, ErrPublishedAtNotFound
}

func ExtractCommunityPublishedAtFromHTML(html string) (*time.Time, error) {
	publishedAt, err := ExtractPublishedAtFromHTML(html)
	if errors.Is(err, ErrPublishedAtNotFound) {
		return nil, ErrCommunityPublishedAtNotFound
	}
	if err != nil {
		return nil, err
	}
	return publishedAt, nil
}

func collectPublishedAtCandidates(html string) []string {
	collector := newPublishedAtCandidateCollector()
	collector.collectMetaCandidates(html)
	collector.collectJSONCandidates(html)

	return collector.candidates
}

type publishedAtCandidateCollector struct {
	candidates []string
	seen       map[string]struct{}
}

func newPublishedAtCandidateCollector() *publishedAtCandidateCollector {
	return &publishedAtCandidateCollector{
		candidates: make([]string, 0, 8),
		seen:       make(map[string]struct{}, 8),
	}
}

func (collector *publishedAtCandidateCollector) add(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, exists := collector.seen[value]; exists {
		return
	}
	collector.seen[value] = struct{}{}
	collector.candidates = append(collector.candidates, value)
}

func (collector *publishedAtCandidateCollector) collectMetaCandidates(html string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return
	}

	doc.Find("meta").Each(func(_ int, selection *goquery.Selection) {
		collector.collectMetaCandidate(selection)
	})
}

func (collector *publishedAtCandidateCollector) collectMetaCandidate(selection *goquery.Selection) {
	content := strings.TrimSpace(selection.AttrOr("content", ""))
	if content == "" {
		return
	}

	for _, attr := range []string{"itemprop", "property", "name"} {
		switch strings.ToLower(strings.TrimSpace(selection.AttrOr(attr, ""))) {
		case "datepublished", "datecreated", "uploaddate", "publishdate":
			collector.add(content)
		}
	}
}

func (collector *publishedAtCandidateCollector) collectJSONCandidates(html string) {
	for _, pattern := range communityPublishedAtJSONPatterns {
		matches := pattern.FindAllStringSubmatch(html, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			collector.add(match[1])
		}
	}
}

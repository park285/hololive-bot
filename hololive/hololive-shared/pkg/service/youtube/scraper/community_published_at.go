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
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

var ErrPublishedAtNotFound = errors.New("published_at not found")
var ErrCommunityPublishedAtNotFound = errors.New("community published_at not found")

var communityPublishedAtJSONPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)"(?:datePublished|dateCreated|uploadDate|publishDate)"\s*:\s*"([^"]+)"`),
}

func normalizePublishedAtCandidate(value string) (*time.Time, bool) {
	return yttimestamp.ParsePublishedAt(value)
}

func extractPublishedAtFromHTML(html string) (*time.Time, error) {
	for _, candidate := range collectPublishedAtCandidates(html) {
		publishedAt, ok := normalizePublishedAtCandidate(candidate)
		if ok {
			return publishedAt, nil
		}
	}

	return nil, ErrPublishedAtNotFound
}

func extractCommunityPublishedAtFromHTML(html string) (*time.Time, error) {
	publishedAt, err := extractPublishedAtFromHTML(html)
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

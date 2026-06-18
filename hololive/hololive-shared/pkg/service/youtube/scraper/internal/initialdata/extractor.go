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

package initialdata

import (
	"errors"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const maxYtInitialDataCandidates = 8

const (
	maxYtInitialDataAssignmentScanBytes  = 256
	maxYtInitialDataObjectStartScanBytes = 512
)

const (
	ytJSONObjectOpen  byte = 123
	ytJSONObjectClose byte = 125
	ytDoubleQuote     byte = 34
	ytSingleQuote     byte = 39
	ytEscape          byte = 92
)

var ytInitialDataPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.+?\})\s*;\s*</script>`),
	regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.+?\})\s*</script>`),
	regexp.MustCompile(`(?s)window\["ytInitialData"\]\s*=\s*(\{.+?\})\s*;`),
}

var ytInitialDataAnchors = []string{
	"var ytInitialData",
	"window.ytInitialData",
	"self.ytInitialData",
	`window["ytInitialData"]`,
	`window['ytInitialData']`,
}

var ytInitialDataDOMFallbackAnchors = []string{
	"window.ytInitialData",
	"ytInitialData",
}

var ErrNotFound = errors.New("ytInitialData not found in HTML")

// extractYtInitialData: YouTube HTML에서 ytInitialData JSON을 추출
func Extract(html string) (string, error) {
	candidates := collectYtInitialDataCandidates(html)
	if best, ok := pickBestYtInitialDataCandidate(candidates); ok {
		return best, nil
	}

	return "", ErrNotFound
}

func collectYtInitialDataCandidates(html string) []string {
	collector := newYtInitialDataCandidateCollector(maxYtInitialDataCandidates)
	collectAnchorCandidates(html, collector)
	collectPatternCandidates(html, collector)
	if len(collector.values) == 0 {
		collectDOMScriptCandidates(html, collector)
	}
	return collector.values
}

type ytInitialDataCandidateCollector struct {
	values []string
	seen   map[string]struct{}
	limit  int
}

func newYtInitialDataCandidateCollector(limit int) *ytInitialDataCandidateCollector {
	return &ytInitialDataCandidateCollector{
		values: make([]string, 0, limit),
		seen:   make(map[string]struct{}, limit),
		limit:  limit,
	}
}

func (c *ytInitialDataCandidateCollector) full() bool {
	return len(c.values) >= c.limit
}

func (c *ytInitialDataCandidateCollector) add(candidate string) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return
	}
	if _, exists := c.seen[candidate]; exists {
		return
	}
	c.seen[candidate] = struct{}{}
	c.values = append(c.values, candidate)
}

func collectAnchorCandidates(html string, collector *ytInitialDataCandidateCollector) {
	for _, anchor := range ytInitialDataAnchors {
		if collector.full() {
			return
		}
		scanAnchorCandidates(html, anchor, collector)
	}
}

func scanAnchorCandidates(html, anchor string, collector *ytInitialDataCandidateCollector) {
	searchFrom := 0
	for searchFrom < len(html) && !collector.full() {
		candidate, nextSearch, ok := findNextAnchorCandidate(html, anchor, searchFrom)
		if !ok {
			if nextSearch <= searchFrom {
				return
			}
			searchFrom = nextSearch
			continue
		}
		collector.add(candidate)
		searchFrom = nextSearch
	}
}

func findNextAnchorCandidate(html, anchor string, searchFrom int) (result1 string, result2 int, result3 bool) {
	idx := strings.Index(html[searchFrom:], anchor)
	if idx < 0 {
		return "", len(html), false
	}
	idx += searchFrom

	assignmentEnd := min(len(html), idx+maxYtInitialDataAssignmentScanBytes)
	eqOffset := strings.Index(html[idx:assignmentEnd], "=")
	if eqOffset < 0 {
		return "", idx + len(anchor), false
	}
	eqIdx := idx + eqOffset
	if eqIdx+1 >= len(html) {
		return "", len(html), false
	}

	objectSearchEnd := min(len(html), eqIdx+1+maxYtInitialDataObjectStartScanBytes)
	objOffset := strings.IndexByte(html[eqIdx+1:objectSearchEnd], ytJSONObjectOpen)
	if objOffset < 0 {
		return "", eqIdx + 1, false
	}
	objStart := eqIdx + 1 + objOffset

	objEnd, ok := findJSONObjectEnd(html, objStart)
	if !ok {
		return "", objStart + 1, false
	}

	return html[objStart : objEnd+1], objEnd + 1, true
}

func collectPatternCandidates(html string, collector *ytInitialDataCandidateCollector) {
	for _, pattern := range ytInitialDataPatterns {
		if collector.full() {
			return
		}
		appendPatternMatches(pattern.FindAllStringSubmatch(html, -1), collector)
	}
}

func appendPatternMatches(matches [][]string, collector *ytInitialDataCandidateCollector) {
	for _, match := range matches {
		if collector.full() {
			return
		}
		if len(match) < 2 {
			continue
		}
		collector.add(match[1])
	}
}

func collectDOMScriptCandidates(html string, collector *ytInitialDataCandidateCollector) {
	if strings.TrimSpace(html) == "" || collector.full() {
		return
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return
	}

	doc.Find("script").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		if collector.full() {
			return false
		}

		scriptBody := strings.TrimSpace(selection.Text())
		if scriptBody == "" {
			return true
		}

		collectAnchorCandidates(scriptBody, collector)
		collectPatternCandidates(scriptBody, collector)
		collectGenericDOMAnchorCandidates(scriptBody, collector)

		return !collector.full()
	})
}

func collectGenericDOMAnchorCandidates(scriptBody string, collector *ytInitialDataCandidateCollector) {
	for _, anchor := range ytInitialDataDOMFallbackAnchors {
		if collector.full() {
			return
		}
		scanAnchorCandidates(scriptBody, anchor, collector)
	}
}

func findJSONObjectEnd(src string, start int) (int, bool) {
	if start < 0 || start >= len(src) || src[start] != ytJSONObjectOpen {
		return 0, false
	}

	scanner := ytJSONObjectEndScanner{}
	for i := start; i < len(src); i++ {
		if scanner.consume(src[i]) {
			return i, true
		}
	}

	return 0, false
}

type ytJSONObjectEndScanner struct {
	depth    int
	inString bool
	escaped  bool
	quote    byte
}

func (s *ytJSONObjectEndScanner) consume(ch byte) bool {
	if s.inString {
		s.consumeStringByte(ch)
		return false
	}
	return s.consumeStructuralByte(ch)
}

func (s *ytJSONObjectEndScanner) consumeStringByte(ch byte) {
	if s.escaped {
		s.escaped = false
		return
	}
	if ch == ytEscape {
		s.escaped = true
		return
	}
	if ch == s.quote {
		s.inString = false
	}
}

func (s *ytJSONObjectEndScanner) consumeStructuralByte(ch byte) bool {
	if ch == ytDoubleQuote || ch == ytSingleQuote {
		s.inString = true
		s.quote = ch
		return false
	}
	if ch == ytJSONObjectOpen {
		s.depth++
		return false
	}
	if ch != ytJSONObjectClose {
		return false
	}

	s.depth--
	return s.depth == 0
}

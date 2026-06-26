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

package summarizer

import (
	"fmt"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/model"
)

func TestBuildSearchQuery_ContainsExpandedScopes(t *testing.T) {
	query := buildSearchQuery(SummaryTypeWeekly, "2026-02-16")

	assertContains(t, query, "site:aniplus.co.kr")
	assertContains(t, query, "site:hololive-official-cardgame.com")
	assertContains(t, query, "HOLOSTARSen")
	assertContains(t, query, "ANIPLUS")
}

func TestBuildORScope_EmptyItems(t *testing.T) {
	got := buildORScope("site:", []string{"", "  "})
	if got != "" {
		t.Fatalf("buildORScope() = %q, want empty", got)
	}
}

func TestBuildKRPartnerSearchQuery(t *testing.T) {
	query := buildKRPartnerSearchQuery("2026-02")

	assertContains(t, query, "ANIPLUS")
	assertContains(t, query, "aniplus.co.kr")
	assertContains(t, query, "live viewing")
	assertContains(t, query, "2026-02")
}

func TestDedupeSearchResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []model.SearchResult
		wantLen  int
		wantURLs []string
	}{
		{
			name: "duplicate URL removed",
			input: []model.SearchResult{
				{Title: "A", URL: "https://example.com/1"},
				{Title: "B", URL: "https://example.com/1"},
				{Title: "C", URL: "https://example.com/2"},
			},
			wantLen:  2,
			wantURLs: []string{"https://example.com/1", "https://example.com/2"},
		},
		{
			name: "empty URL uses title+date composite key",
			input: []model.SearchResult{
				{Title: "Same Title", URL: "", PublishedDate: "2026-02-01"},
				{Title: "Same Title", URL: "", PublishedDate: "2026-02-01"},
			},
			wantLen: 1,
		},
		{
			name: "same title different date kept separate",
			input: []model.SearchResult{
				{Title: "Event", URL: "", PublishedDate: "2026-02-01"},
				{Title: "Event", URL: "", PublishedDate: "2026-03-01"},
			},
			wantLen: 2,
		},
		{
			name:    "empty input",
			input:   []model.SearchResult{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeSearchResults(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("dedupeSearchResults() returned %d items, want %d", len(got), tt.wantLen)
			}
			for i, wantURL := range tt.wantURLs {
				if i < len(got) && got[i].URL != wantURL {
					t.Errorf("result[%d].URL = %q, want %q", i, got[i].URL, wantURL)
				}
			}
		})
	}
}

func TestDedupeSearchResults_MaxCap(t *testing.T) {
	// 15건 입력 → maxSearchResults(10) 이하 반환
	input := make([]model.SearchResult, 15)
	for i := range input {
		input[i] = model.SearchResult{
			Title: fmt.Sprintf("Result %d", i),
			URL:   fmt.Sprintf("https://example.com/%d", i),
		}
	}

	capped := capSearchResults(input, maxSearchResults)
	if len(capped) > maxSearchResults {
		t.Errorf("capSearchResults() returned %d items, want <= %d", len(capped), maxSearchResults)
	}
	if len(capped) != maxSearchResults {
		t.Errorf("capSearchResults() returned %d items, want exactly %d", len(capped), maxSearchResults)
	}
}

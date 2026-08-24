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
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *EventSummarizer) buildSummaryResponse(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
) (*summaryResponse, error) {
	sysPrompt, err := getSystemPrompt(summaryType)
	if err != nil {
		return nil, fmt.Errorf("get system prompt: %w", err)
	}

	userPrompt := buildUserPrompt(events, summaryType, periodKey, searchContext)
	schema := summaryResponseSchema()

	rawJSON, err := s.llm.GenerateJSON(ctx, sysPrompt, userPrompt, schema)
	if err != nil {
		return nil, fmt.Errorf("generate summary json: %w", err)
	}

	var resp summaryResponse

	if err := jsonv2.Unmarshal([]byte(rawJSON), &resp); err != nil {
		return nil, fmt.Errorf("parse summary json: %w", err)
	}

	return &resp, nil
}

func filterTrustedDiscoveredEvents(input []discoveredEvent) []discoveredEvent {
	if len(input) == 0 {
		return input
	}

	filtered := make([]discoveredEvent, 0, len(input))
	for i := range input {
		item := input[i]
		if isTrustedDiscoveredSource(item.Source) {
			filtered = append(filtered, item)
		}
	}

	return filtered
}

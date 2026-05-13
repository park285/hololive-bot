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
	"fmt"
	"log/slog"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *EventSummarizer) runConsensus(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
	primary *summaryResponse,
) (*summaryResponse, bool) {
	if primary == nil || s.reviewer == nil {
		return primary, false
	}

	verdict, needsAdjudication := s.reviewConsensusVerdict(ctx, events, summaryType, periodKey, primary)
	if !needsAdjudication {
		return primary, false
	}

	return s.applyConsensusAdjudication(ctx, events, summaryType, periodKey, searchContext, primary, verdict)
}

func (s *EventSummarizer) reviewConsensusVerdict(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	primary *summaryResponse,
) (*consensus.ReviewVerdict, bool) {
	reviewCtx, cancel, ok := deriveConsensusBudget(ctx, s.consensus.ReviewTimeout, 250*time.Millisecond)
	if !ok {
		s.logger.Warn("major event consensus skipped: insufficient budget for review")
		return nil, false
	}
	defer cancel()

	verdict, err := s.reviewSummary(reviewCtx, events, summaryType, periodKey, primary)
	if err != nil {
		s.logger.Warn("major event consensus review failed; keep primary",
			slog.String("error", err.Error()))
		return nil, false
	}
	if !consensus.NeedsAdjudication(verdict, s.consensus.ConfidenceThreshold) {
		s.logger.Info("major event consensus review passed",
			slog.Bool("approved", verdict.Approved),
			slog.Float64("confidence", verdict.Confidence))
		return nil, false
	}

	return verdict, true
}

func (s *EventSummarizer) applyConsensusAdjudication(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
	primary *summaryResponse,
	verdict *consensus.ReviewVerdict,
) (*summaryResponse, bool) {
	if s.adjudicator == nil {
		s.logger.Info("major event consensus adjudicator not configured; keep primary")
		return primary, false
	}

	adjCtx, adjCancel, ok := deriveConsensusBudget(ctx, s.consensus.AdjudicateTimeout, 250*time.Millisecond)
	if !ok {
		s.logger.Warn("major event consensus skipped: insufficient budget for adjudication")
		return primary, false
	}
	defer adjCancel()

	adjusted, err := s.adjudicateSummary(adjCtx, events, summaryType, periodKey, searchContext, primary, verdict)
	if err != nil {
		s.logger.Warn("major event consensus adjudication failed; keep primary",
			slog.String("error", err.Error()))
		return primary, false
	}
	s.logger.Info("major event consensus adjudication applied",
		slog.Float64("confidence", verdict.Confidence),
		slog.Int("issues", len(verdict.Issues)))
	return adjusted, true
}

func deriveConsensusBudget(parent context.Context, requested, reserve time.Duration) (context.Context, context.CancelFunc, bool) {
	if requested <= 0 {
		requested = time.Second
	}
	if reserve < 0 {
		reserve = 0
	}

	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline) - reserve
		if remaining <= 0 {
			return nil, nil, false
		}
		if remaining < requested {
			requested = remaining
		}
	}

	ctx, cancel := context.WithTimeout(parent, requested)
	return ctx, cancel, true
}

func (s *EventSummarizer) reviewSummary(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	primary *summaryResponse,
) (*consensus.ReviewVerdict, error) {
	primaryJSON, _ := json.Marshal(primary)

	raw, err := s.reviewer.GenerateJSON(
		ctx,
		reviewSummarySystemPrompt(),
		buildReviewSummaryUserPrompt(events, summaryType, periodKey, string(primaryJSON)),
		reviewSummarySchema(),
	)
	if err != nil {
		return nil, fmt.Errorf("reviewer llm call: %w", err)
	}

	var verdict consensus.ReviewVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return nil, fmt.Errorf("parse reviewer verdict: %w", err)
	}

	for i := range verdict.Issues {
		verdict.Issues[i].Severity = consensus.NormalizeSeverity(verdict.Issues[i].Severity)
	}
	return &verdict, nil
}

func (s *EventSummarizer) adjudicateSummary(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
	primary *summaryResponse,
	verdict *consensus.ReviewVerdict,
) (*summaryResponse, error) {
	primaryJSON, _ := json.Marshal(primary)
	verdictJSON, _ := json.Marshal(verdict)

	raw, err := s.adjudicator.GenerateJSON(
		ctx,
		adjudicateSummarySystemPrompt(),
		buildAdjudicateSummaryUserPrompt(events, summaryType, periodKey, searchContext, string(primaryJSON), string(verdictJSON)),
		summaryResponseSchema(),
	)
	if err != nil {
		return nil, fmt.Errorf("adjudicator llm call: %w", err)
	}

	var resp summaryResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse adjudicator summary: %w", err)
	}
	return &resp, nil
}

func (s *EventSummarizer) runFinalOutputReview(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, assembled string,
) (string, bool) {
	trimmed := strings.TrimSpace(assembled)
	if trimmed == "" || s.reviewer == nil {
		return assembled, false
	}

	reviewCtx, cancel := context.WithTimeout(ctx, s.consensus.ReviewTimeout)
	defer cancel()

	raw, err := s.reviewer.GenerateJSON(
		reviewCtx,
		finalOutputReviewSystemPrompt(),
		buildFinalOutputReviewUserPrompt(events, summaryType, periodKey, trimmed),
		finalOutputReviewSchema(),
	)
	if err != nil {
		s.logger.Warn("major event final output review failed; keep assembled",
			slog.String("error", err.Error()))
		return assembled, false
	}

	var reviewed consensus.FinalOutputReviewResponse
	if err := json.Unmarshal([]byte(raw), &reviewed); err != nil {
		s.logger.Warn("major event final output review parse failed; keep assembled",
			slog.String("error", err.Error()))
		return assembled, false
	}

	reviewedSummary := strings.TrimSpace(reviewed.Summary)
	if reviewedSummary == "" || reviewedSummary == trimmed {
		return assembled, false
	}

	s.logger.Info("major event final output review applied",
		slog.Int("before_length", len(trimmed)),
		slog.Int("after_length", len(reviewedSummary)))
	return reviewedSummary, true
}

func reviewSummarySystemPrompt() string {
	return `You are a strict factual reviewer for hololive major-event summaries.
Check if the summary JSON matches input events, date ranges, and trusted-source rules.
Return only JSON according to schema.`
}

func finalOutputReviewSystemPrompt() string {
	return `You are the final output reviewer for hololive major-event notifications.
You must keep factual content unchanged while removing duplicate or redundant lines.
Do not add new events. Do not remove valid links.
Return only JSON according to schema.`
}

func adjudicateSummarySystemPrompt() string {
	return `You are a hololive major-event adjudicator.
A reviewer found issues in the primary summary.
Return corrected summary JSON only, using the exact output schema.`
}

func buildReviewSummaryUserPrompt(
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, primarySummaryJSON string,
) string {
	eventBytes, _ := json.Marshal(events)
	return fmt.Sprintf(`summary_type=%s
period_key=%s

input_events:
%s

primary_summary_json:
%s

Tasks:
1) Verify no missing major event in highlights.
2) Verify ongoing events are placed in ongoing_events.
3) Verify discovered_events have trusted source and are not duplicates.
4) Verify date/member/link consistency.
5) Output verdict JSON only.`, summaryType, periodKey, string(eventBytes), primarySummaryJSON)
}

func buildAdjudicateSummaryUserPrompt(
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext, primarySummaryJSON, verdictJSON string,
) string {
	basePrompt := buildUserPrompt(events, summaryType, periodKey, searchContext)
	return fmt.Sprintf(`primary_summary_json:
%s

review_verdict_json:
%s

original_generation_context:
%s

Please regenerate corrected summary JSON by fixing reviewer issues.`, primarySummaryJSON, verdictJSON, basePrompt)
}

func buildFinalOutputReviewUserPrompt(
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, assembled string,
) string {
	eventBytes, _ := json.Marshal(events)
	return fmt.Sprintf(`summary_type=%s
period_key=%s

input_events:
%s

assembled_text:
%s

Tasks:
1) Remove duplicated event lines/sections.
2) Keep factual content identical to input events and discovered events already present.
3) Keep section labels if present: [기간 행사], [추가 발견].
4) Keep all valid links.
5) Return JSON only.`, summaryType, periodKey, string(eventBytes), assembled)
}

func reviewSummarySchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": map[string]any{
				"type": "boolean",
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"issues": map[string]any{
				"type":  "array",
				"items": reviewIssueSchema(),
			},
		},
		"required": []string{"approved", "confidence", "issues"},
	}
}

func reviewIssueSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"field": map[string]any{
				"type": "string",
			},
			"item_index": map[string]any{
				"type": "integer",
			},
			"severity": map[string]any{
				"type": "string",
				"enum": []string{"critical", "warning", "info"},
			},
			"description": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"field", "item_index", "severity", "description"},
	}
}

func finalOutputReviewSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "final deduplicated summary text",
			},
		},
		"required": []string{"summary"},
	}
}

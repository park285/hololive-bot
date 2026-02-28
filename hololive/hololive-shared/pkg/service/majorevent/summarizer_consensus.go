package majorevent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type summaryReviewIssue struct {
	Field       string `json:"field"`
	ItemIndex   int    `json:"item_index"`
	Severity    string `json:"severity"` // critical, warning, info
	Description string `json:"description"`
}

type summaryReviewVerdict struct {
	Approved   bool                 `json:"approved"`
	Confidence float64              `json:"confidence"`
	Issues     []summaryReviewIssue `json:"issues"`
}

type finalOutputReviewResponse struct {
	Summary string `json:"summary"`
}

func (s *EventSummarizer) runConsensus(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
	primary *summaryResponse,
) (*summaryResponse, bool) {
	// consensus: deadline budget TODO
	if primary == nil || s.reviewer == nil {
		return primary, false
	}

	reviewCtx, cancel := context.WithTimeout(ctx, s.consensus.ReviewTimeout)
	defer cancel()

	verdict, err := s.reviewSummary(reviewCtx, events, summaryType, periodKey, primary)
	if err != nil {
		s.logger.Warn("major event consensus review failed; keep primary",
			slog.String("error", err.Error()))
		return primary, false
	}
	if verdict == nil {
		return primary, false
	}

	if !needsSummaryAdjudication(verdict, s.consensus.ConfidenceThreshold) {
		s.logger.Info("major event consensus review passed",
			slog.Bool("approved", verdict.Approved),
			slog.Float64("confidence", verdict.Confidence))
		return primary, false
	}

	if s.adjudicator == nil {
		s.logger.Info("major event consensus adjudicator not configured; keep primary")
		return primary, false
	}

	adjCtx, adjCancel := context.WithTimeout(ctx, s.consensus.AdjudicateTimeout)
	defer adjCancel()

	adjusted, err := s.adjudicateSummary(adjCtx, events, summaryType, periodKey, searchContext, primary, verdict)
	if err != nil {
		s.logger.Warn("major event consensus adjudication failed; keep primary",
			slog.String("error", err.Error()))
		return primary, false
	}
	if adjusted == nil {
		return primary, false
	}

	s.logger.Info("major event consensus adjudication applied",
		slog.Float64("confidence", verdict.Confidence),
		slog.Int("issues", len(verdict.Issues)))
	return adjusted, true
}

func (s *EventSummarizer) reviewSummary(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey string,
	primary *summaryResponse,
) (*summaryReviewVerdict, error) {
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

	var verdict summaryReviewVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		return nil, fmt.Errorf("parse reviewer verdict: %w", err)
	}

	for i := range verdict.Issues {
		verdict.Issues[i].Severity = normalizeSeverity(verdict.Issues[i].Severity)
	}
	return &verdict, nil
}

func (s *EventSummarizer) adjudicateSummary(
	ctx context.Context,
	events []domain.MajorEvent,
	summaryType SummaryType,
	periodKey, searchContext string,
	primary *summaryResponse,
	verdict *summaryReviewVerdict,
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

	var reviewed finalOutputReviewResponse
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

func needsSummaryAdjudication(verdict *summaryReviewVerdict, confidenceThreshold float64) bool {
	if verdict == nil {
		return false
	}
	if !verdict.Approved {
		return true
	}
	for _, issue := range verdict.Issues {
		if issue.Severity == "critical" {
			return true
		}
	}
	return verdict.Confidence < confidenceThreshold
}

func normalizeSeverity(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical", "warning", "info":
		return strings.ToLower(strings.TrimSpace(severity))
	default:
		return "info"
	}
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
				"type": "array",
				"items": map[string]any{
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
				},
			},
		},
		"required": []string{"approved", "confidence", "issues"},
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

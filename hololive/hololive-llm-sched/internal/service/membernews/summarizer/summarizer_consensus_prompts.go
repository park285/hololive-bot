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

	json "github.com/park285/shared-go/pkg/json"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

func reviewSystemPrompt() string {
	return `You are a fact-checking reviewer for hololive member-news summaries.
Your task is to verify the PRIMARY summary against the original candidate data.

Check for:
1. source_url accuracy: URLs must exactly match candidate source_urls (no fabrication or modification)
2. member attribution: Each item must attribute to the correct member(s) from candidates
3. category accuracy: Category must match the event type from candidates
4. factual consistency: Summary text must not introduce facts not present in candidates

Rules:
- Output MUST be valid JSON matching the provided schema only.
- Set approved=true only if ALL items pass ALL checks.
- confidence: your certainty in the review (0.0 = uncertain, 1.0 = fully certain).
- For each issue found, specify field, item_index (-1 for global), severity (critical/warning/info), and description.
- severity=critical: URL fabrication, wrong member, made-up facts.
- severity=warning: minor category mismatch, slight rewording concern.
- severity=info: stylistic suggestions.`
}

func buildReviewUserPrompt(input model.SummarizeInput, digest *model.Digest) string {
	candidatesJSON, _ := json.Marshal(buildPromptCandidates(input))
	digestJSON, _ := json.Marshal(digest)

	return fmt.Sprintf(`ORIGINAL CANDIDATES:
%s

PRIMARY SUMMARY TO REVIEW:
%s

Review the summary against the original candidates. Return only schema JSON.`,
		string(candidatesJSON),
		string(digestJSON),
	)
}

func buildPromptCandidates(input model.SummarizeInput) []promptCandidate {
	candidates := make([]promptCandidate, 0, len(input.Candidates))
	for i := range input.Candidates {
		candidate := &input.Candidates[i]
		candidates = append(candidates, promptCandidate{
			Member:     candidate.MemberText,
			Category:   string(candidate.Category),
			Title:      candidate.Candidate.Title,
			Date:       candidate.EffectiveDate.In(kst).Format("2006-01-02"),
			SourceURL:  candidate.SourceURL,
			SourceTier: string(candidate.SourceTier),
			Summary:    candidate.Candidate.Description,
		})
	}
	return candidates
}

func reviewVerdictSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"approved":   typeSchema("boolean"),
			"issues":     arraySchema(reviewVerdictIssueSchema()),
			"confidence": numberRangeSchema(0.0, 1.0),
		},
		[]string{"approved", "issues", "confidence"},
	)
}

func reviewVerdictIssueSchema() map[string]any {
	return objectSchema(
		map[string]any{
			"field":       typeSchema("string"),
			"item_index":  typeSchema("integer"),
			"severity":    enumSchema("string", []string{"critical", "warning", "info"}),
			"description": typeSchema("string"),
		},
		[]string{"field", "item_index", "severity", "description"},
	)
}

func adjudicatorSystemPrompt() string {
	return `You are a hololive member-news adjudicator.
A primary summary was generated but the reviewer found issues.
Your task is to produce a CORRECTED summary that fixes the identified issues.

Rules:
- Output MUST be valid JSON matching the provided schema only.
- Use ONLY the original candidate data as source of truth.
- Fix all critical issues identified by the reviewer.
- source_url MUST exactly match one of the candidate source_urls.
- Korean summaries only, factual and concise.
- Do not guess unknown facts.`
}

func buildAdjudicatorUserPrompt(
	input model.SummarizeInput,
	digest *model.Digest,
	verdict *consensus.ReviewVerdict,
) string {
	candidatesJSON, _ := json.Marshal(buildPromptCandidates(input))
	digestJSON, _ := json.Marshal(digest)
	verdictJSON, _ := json.Marshal(verdict)

	return fmt.Sprintf(`ORIGINAL CANDIDATES:
%s

PRIMARY SUMMARY (has issues):
%s

REVIEWER VERDICT:
%s

Produce a corrected summary that fixes the identified issues. Return only schema JSON.`,
		string(candidatesJSON),
		string(digestJSON),
		string(verdictJSON),
	)
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func typeSchema(schemaType string) map[string]any {
	return map[string]any{"type": schemaType}
}

func enumSchema(schemaType string, values []string) map[string]any {
	return map[string]any{
		"type": schemaType,
		"enum": values,
	}
}

func numberRangeSchema(minimum, maximum float64) map[string]any {
	return map[string]any{
		"type":    "number",
		"minimum": minimum,
		"maximum": maximum,
	}
}

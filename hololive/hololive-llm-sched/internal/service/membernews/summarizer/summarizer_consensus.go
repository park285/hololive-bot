package summarizer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-llm-sched/internal/service/consensus"
	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
)

// ConsensusSummarizer: Primary → Reviewer → Adjudicator(조건부) 3단계 consensus wrapper.
type ConsensusSummarizer struct {
	primary     model.Summarizer
	reviewer    LLMClient
	adjudicator LLMClient // nil이면 stage 3 스킵
	validator   model.SourceURLValidator
	config      consensus.Config
	logger      *slog.Logger
}

// NewConsensusSummarizer: consensus 요약기 생성.
func NewConsensusSummarizer(
	primary model.Summarizer,
	reviewer LLMClient,
	adjudicator LLMClient,
	validator model.SourceURLValidator,
	cfg consensus.Config,
	logger *slog.Logger,
) *ConsensusSummarizer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConsensusSummarizer{
		primary:     primary,
		reviewer:    reviewer,
		adjudicator: adjudicator,
		validator:   validator,
		config:      cfg,
		logger:      logger,
	}
}

// Summarize: Summarizer 인터페이스 구현. Primary → Reviewer → Adjudicator(조건부) 파이프라인.
func (c *ConsensusSummarizer) Summarize(ctx context.Context, input model.SummarizeInput) (*model.Digest, error) {
	pipelineStart := time.Now()

	// Stage 1: Primary
	primaryDigest, err := c.primary.Summarize(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("consensus primary: %w", err)
	}
	if primaryDigest == nil || len(primaryDigest.TopItems) == 0 {
		c.logger.Info("Consensus stage 1: primary returned empty, skipping review")
		return primaryDigest, nil
	}

	// reviewer nil → consensus 비활성 상태로 primary 직접 반환
	if c.reviewer == nil {
		return primaryDigest, nil
	}

	c.logger.Info("Consensus stage 1: primary complete",
		slog.Duration("latency", time.Since(pipelineStart)),
		slog.Int("items", len(primaryDigest.TopItems)),
	)

	// Stage 2: Review
	verdict := c.runReview(ctx, input, primaryDigest)
	if verdict == nil {
		return primaryDigest, nil
	}

	// 결정표 우선순위 3, 6: adjudication 불필요
	if !consensus.NeedsAdjudication(verdict, c.config.ConfidenceThreshold) {
		c.logger.Info("Consensus pipeline: review passed, returning primary",
			slog.Duration("total_latency", time.Since(pipelineStart)),
			slog.Int("stages_used", 2),
		)
		return primaryDigest, nil
	}

	// Stage 3: Adjudicate
	adjDigest := c.runAdjudication(ctx, input, primaryDigest, verdict, pipelineStart)
	if adjDigest != nil {
		return adjDigest, nil
	}
	return primaryDigest, nil
}

// runReview: Stage 2 reviewer 호출 및 결과 평가. nil 반환 시 primary 사용.
func (c *ConsensusSummarizer) runReview(
	ctx context.Context,
	input model.SummarizeInput,
	primaryDigest *model.Digest,
) *consensus.ReviewVerdict {
	reviewStart := time.Now()
	reviewCtx, reviewCancel := context.WithTimeout(ctx, c.config.ReviewTimeout)
	defer reviewCancel()

	verdict, err := c.review(reviewCtx, input, primaryDigest)
	reviewLatency := time.Since(reviewStart)

	// 결정표 우선순위 1: reviewer 호출 실패
	if err != nil {
		c.logger.Warn("Consensus stage 2: reviewer failed, returning primary",
			slog.String("error", err.Error()),
			slog.Duration("latency", reviewLatency),
		)
		return nil
	}

	// 결정표 우선순위 2: ReviewVerdict JSON 파싱 실패
	if verdict == nil {
		c.logger.Warn("Consensus stage 2: verdict parse failed, returning primary",
			slog.Duration("latency", reviewLatency),
		)
		return nil
	}

	c.logger.Info("Consensus stage 2: review complete",
		slog.Duration("latency", reviewLatency),
		slog.Bool("approved", verdict.Approved),
		slog.Float64("confidence", verdict.Confidence),
		slog.Int("issues", len(verdict.Issues)),
	)
	return verdict
}

// runAdjudication: Stage 3 adjudicator 호출. nil 반환 시 primary 사용.
func (c *ConsensusSummarizer) runAdjudication(
	ctx context.Context, input model.SummarizeInput,
	primaryDigest *model.Digest, verdict *consensus.ReviewVerdict,
	pipelineStart time.Time,
) *model.Digest {
	triggerReason := "low_confidence"
	if consensus.HasCriticalIssues(verdict.Issues) {
		triggerReason = "critical_issues"
	}
	c.logger.Info("Consensus stage 3: adjudication triggered", slog.String("reason", triggerReason))

	if c.adjudicator == nil {
		c.logger.Info("Consensus stage 3: adjudicator not configured, returning primary")
		return nil
	}

	adjStart := time.Now()
	adjCtx, adjCancel := context.WithTimeout(ctx, c.config.AdjudicateTimeout)
	defer adjCancel()

	adjResponse, err := c.adjudicate(adjCtx, input, primaryDigest, verdict)
	adjLatency := time.Since(adjStart)

	if err != nil {
		c.logger.Warn("Consensus stage 3: adjudicator failed, returning primary",
			slog.String("error", err.Error()), slog.Duration("latency", adjLatency))
		return nil
	}
	if adjResponse == nil {
		c.logger.Warn("Consensus stage 3: adjudicator parse failed, returning primary",
			slog.Duration("latency", adjLatency))
		return nil
	}

	adjDigest := validateAndBuildDigestFromResponse(input, adjResponse, c.validator)
	if len(adjDigest.TopItems) == 0 {
		c.logger.Warn("Consensus stage 3: adjudicator output validation dropped all items, returning primary",
			slog.Duration("latency", adjLatency))
		return nil
	}

	c.logger.Info("Consensus pipeline: adjudicator result accepted",
		slog.Duration("total_latency", time.Since(pipelineStart)),
		slog.Int("stages_used", 3),
		slog.Int("adjudicator_items", len(adjDigest.TopItems)),
	)
	return adjDigest
}

// review: reviewer LLM 호출. verdict 파싱 실패 시 nil, nil 반환.
func (c *ConsensusSummarizer) review(
	ctx context.Context,
	input model.SummarizeInput,
	digest *model.Digest,
) (*consensus.ReviewVerdict, error) {
	raw, err := c.reviewer.GenerateJSON(
		ctx,
		reviewSystemPrompt(),
		buildReviewUserPrompt(input, digest),
		reviewVerdictSchema(),
	)
	if err != nil {
		return nil, fmt.Errorf("reviewer LLM call: %w", err)
	}

	var verdict consensus.ReviewVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		c.logger.Warn("Consensus review: JSON parse failed",
			slog.String("error", err.Error()),
		)
		return nil, nil
	}

	// severity 정규화
	for i := range verdict.Issues {
		verdict.Issues[i].Severity = consensus.NormalizeSeverity(verdict.Issues[i].Severity)
	}

	return &verdict, nil
}

// adjudicate: adjudicator LLM 호출. 파싱 실패 시 nil, nil 반환.
func (c *ConsensusSummarizer) adjudicate(
	ctx context.Context,
	input model.SummarizeInput,
	digest *model.Digest,
	verdict *consensus.ReviewVerdict,
) (*summaryResponse, error) {
	raw, err := c.adjudicator.GenerateJSON(
		ctx,
		adjudicatorSystemPrompt(),
		buildAdjudicatorUserPrompt(input, digest, verdict),
		memberNewsSummarySchema(),
	)
	if err != nil {
		return nil, fmt.Errorf("adjudicator LLM call: %w", err)
	}

	var response summaryResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		c.logger.Warn("Consensus adjudicator: JSON parse failed",
			slog.String("error", err.Error()),
		)
		return nil, nil
	}

	return &response, nil
}

// --- 프롬프트 함수 ---

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
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": map[string]any{"type": "boolean"},
			"issues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"field":       map[string]any{"type": "string"},
						"item_index":  map[string]any{"type": "integer"},
						"severity":    map[string]any{"type": "string", "enum": []string{"critical", "warning", "info"}},
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"field", "item_index", "severity", "description"},
				},
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0.0,
				"maximum": 1.0,
			},
		},
		"required": []string{"approved", "issues", "confidence"},
	}
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

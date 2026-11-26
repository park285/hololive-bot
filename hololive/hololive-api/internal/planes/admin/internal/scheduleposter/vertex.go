package scheduleposter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

var errIncompleteStream = errors.New("schedule poster model stream ended before the model finished")

type streamedResponse struct {
	model     string
	token     []byte
	response  genai.GenerateContentResponse
	candidate *genai.Candidate
	spent     usage
}

func generateContent(
	ctx context.Context,
	models *genai.Models,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) (*genai.GenerateContentResponse, usage, error) {
	config.AutomaticContinuation = new(false)

	streamed := &streamedResponse{model: model}

	for hop := config; hop != nil; {
		token, err := streamed.read(ctx, models, model, contents, hop)
		if err != nil {
			return nil, streamed.spent, err
		}

		hop = continuationConfig(config, token)
	}

	return streamed.finish()
}

func continuationConfig(config *genai.GenerateContentConfig, token []byte) *genai.GenerateContentConfig {
	if len(token) == 0 {
		return nil
	}

	next := *config

	next.ContinuationToken = token

	return &next
}

func (s *streamedResponse) read(
	ctx context.Context,
	models *genai.Models,
	model string,
	contents []*genai.Content,
	config *genai.GenerateContentConfig,
) ([]byte, error) {
	var hopUsage *genai.GenerateContentResponseUsageMetadata

	for chunk, err := range models.GenerateContentStream(ctx, model, contents, config) {
		if err != nil {
			s.closeHop(hopUsage)

			return nil, err
		}

		if chunk != nil && chunk.UsageMetadata != nil {
			hopUsage = chunk.UsageMetadata
		}

		s.merge(chunk)
	}

	s.closeHop(hopUsage)

	return s.continuationToken(), nil
}

func (s *streamedResponse) closeHop(hop *genai.GenerateContentResponseUsageMetadata) {
	s.spent = s.spent.add(s.model, hop)

	if hop == nil {
		return
	}

	if s.response.UsageMetadata == nil {
		s.response.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{}
	}

	total := s.response.UsageMetadata

	total.PromptTokenCount += hop.PromptTokenCount
	total.CandidatesTokenCount += hop.CandidatesTokenCount
	total.ThoughtsTokenCount += hop.ThoughtsTokenCount
	total.TotalTokenCount += hop.TotalTokenCount
}

func (s *streamedResponse) merge(chunk *genai.GenerateContentResponse) {
	if chunk == nil {
		return
	}

	if chunk.PromptFeedback != nil {
		s.response.PromptFeedback = chunk.PromptFeedback
	}

	if chunk.ModelVersion != "" {
		s.response.ModelVersion = chunk.ModelVersion
	}

	if len(chunk.Candidates) == 0 || chunk.Candidates[0] == nil {
		return
	}

	if s.candidate == nil {
		s.candidate = &genai.Candidate{}
		s.response.Candidates = []*genai.Candidate{s.candidate}
	}

	mergeCandidate(s.candidate, chunk.Candidates[0])
}

func (s *streamedResponse) continuationToken() []byte {
	candidate := s.candidate
	if candidate == nil || s.response.PromptFeedback != nil ||
		candidate.FinishReason != genai.FinishReasonContinuation || len(candidate.ContinuationToken) == 0 {
		return nil
	}

	token := candidate.ContinuationToken
	if bytes.Equal(token, s.token) {
		return nil
	}

	s.token = token

	candidate.FinishReason, candidate.FinishMessage, candidate.ContinuationToken = "", "", nil

	return token
}

func (s *streamedResponse) finish() (*genai.GenerateContentResponse, usage, error) {
	switch {
	case s.candidate == nil && s.response.PromptFeedback == nil, s.candidate != nil && !terminalFinishReason(s.candidate.FinishReason):
		return nil, s.spent, errIncompleteStream
	case s.candidate != nil && !completeFinishReason(s.candidate.FinishReason):
		s.candidate.Content = nil
	}

	return &s.response, s.spent, nil
}

func mergeCandidate(merged, chunk *genai.Candidate) {
	if chunk.Content != nil {
		if merged.Content == nil {
			merged.Content = &genai.Content{Role: chunk.Content.Role}
		}

		merged.Content.Parts = append(merged.Content.Parts, chunk.Content.Parts...)
	}

	if len(chunk.ContinuationToken) > 0 {
		merged.ContinuationToken = chunk.ContinuationToken
	}

	if chunk.FinishReason != "" {
		merged.FinishReason = chunk.FinishReason
		merged.FinishMessage = chunk.FinishMessage
	}
}

func completeFinishReason(reason genai.FinishReason) bool {
	return reason == genai.FinishReasonStop || reason == genai.FinishReasonMaxTokens
}

func terminalFinishReason(reason genai.FinishReason) bool {
	return reason != "" && reason != genai.FinishReasonUnspecified && reason != genai.FinishReasonContinuation
}

type usage struct {
	model         string
	prompt        int32
	toolUsePrompt int32
	thoughts      int32
	output        int32
}

func (u usage) add(model string, metadata *genai.GenerateContentResponseUsageMetadata) usage {
	if metadata == nil {
		return u
	}

	u.model = model

	u.prompt += metadata.PromptTokenCount
	u.toolUsePrompt += metadata.ToolUsePromptTokenCount
	u.thoughts += metadata.ThoughtsTokenCount
	u.output += metadata.CandidatesTokenCount

	return u
}

type vertexTranslator struct {
	client *genai.Client
	rows   RowsConfig
}

func newVertexTranslator(ctx context.Context, config TranslationConfig) (*vertexTranslator, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  config.Project,
		Location: config.Location,
		HTTPOptions: genai.HTTPOptions{
			APIVersion:   "v1",
			RetryOptions: &genai.HTTPRetryOptions{Attempts: new(int32(1))},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create vertex ai client with ADC: %w", err)
	}

	return &vertexTranslator{client: client, rows: config.Rows}, nil
}

func posterContents(src source, prompt string) []*genai.Content {
	return []*genai.Content{genai.NewContentFromParts([]*genai.Part{
		genai.NewPartFromBytes(src.Data, src.MIME),
		genai.NewPartFromText(prompt),
	}, genai.RoleUser)}
}

func emptyResponseReason(response *genai.GenerateContentResponse) string {
	var reasons []string

	if feedback := response.PromptFeedback; feedback != nil && feedback.BlockReason != "" {
		reasons = append(reasons, "block reason "+string(feedback.BlockReason))
	}

	if len(response.Candidates) > 0 && response.Candidates[0] != nil && response.Candidates[0].FinishReason != "" {
		reasons = append(reasons, "finish reason "+string(response.Candidates[0].FinishReason))
	}

	if usage := response.UsageMetadata; usage != nil {
		reasons = append(reasons, fmt.Sprintf("thoughts %d tokens, output %d tokens", usage.ThoughtsTokenCount, usage.CandidatesTokenCount))
	}

	if len(reasons) == 0 {
		return "no reason given"
	}

	return strings.Join(reasons, "; ")
}

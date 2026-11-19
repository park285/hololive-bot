package scheduleposter

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

type usage struct {
	model         string
	prompt        int32
	toolUsePrompt int32
	thoughts      int32
	output        int32
}

func responseUsage(model string, response *genai.GenerateContentResponse) usage {
	if response == nil || response.UsageMetadata == nil {
		return usage{model: model}
	}

	metadata := response.UsageMetadata

	return usage{
		model:         model,
		prompt:        metadata.PromptTokenCount,
		toolUsePrompt: metadata.ToolUsePromptTokenCount,
		thoughts:      metadata.ThoughtsTokenCount,
		output:        metadata.CandidatesTokenCount,
	}
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
		reasons = append(reasons, strings.TrimSpace("block reason "+string(feedback.BlockReason)+" "+feedback.BlockReasonMessage))
	}

	if len(response.Candidates) > 0 && response.Candidates[0] != nil && response.Candidates[0].FinishReason != "" {
		candidate := response.Candidates[0]

		reasons = append(reasons, strings.TrimSpace("finish reason "+string(candidate.FinishReason)+" "+candidate.FinishMessage))
	}

	if usage := response.UsageMetadata; usage != nil {
		reasons = append(reasons, fmt.Sprintf("thoughts %d tokens, output %d tokens", usage.ThoughtsTokenCount, usage.CandidatesTokenCount))
	}

	if len(reasons) == 0 {
		return "no reason given"
	}

	return strings.Join(reasons, "; ")
}

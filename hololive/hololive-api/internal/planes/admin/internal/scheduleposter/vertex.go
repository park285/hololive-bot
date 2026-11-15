package scheduleposter

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// usage는 Vertex 응답 하나가 쓴 토큰이다. 응답을 받지 못한 호출은 zero value다.
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

// newVertexTranslator의 인증은 GOOGLE_APPLICATION_CREDENTIALS(ADC)에서 SDK가 읽는다.
func newVertexTranslator(ctx context.Context, config TranslationConfig) (*vertexTranslator, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend:  genai.BackendVertexAI,
		Project:  config.Project,
		Location: config.Location,
		HTTPOptions: genai.HTTPOptions{
			APIVersion: "v1",
			// 재시도는 translationRetryOptions 계약 하나로만 센다.
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

// emptyResponseReason은 결과 없는 응답의 차단·종료 사유를 모은다. 같은 포스터가 매번 막히는지 이 사유로만 가릴 수 있다.
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

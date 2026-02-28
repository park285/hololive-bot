package majorevent

import "context"

// LLMClient: 구조화 JSON 응답을 생성하는 LLM 클라이언트 인터페이스
type LLMClient interface {
	GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error)
}

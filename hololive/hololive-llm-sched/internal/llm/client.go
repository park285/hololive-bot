package llm

import "context"

// Client: 구조화 JSON 응답을 생성하는 LLM 클라이언트 인터페이스
type Client interface {
	GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error)
}

// Options: LLM 클라이언트 생성 옵션
type Options struct {
	SchemaName      string
	Temperature     *float64
	WebSearch       *bool  // nil=기본값(true), false=비활성화
	ChatCompletions bool   // true=Chat Completions API 사용 (cliproxy 호환)
	ReasoningEffort string // reasoning 모델용 사고 깊이 (high, xhigh 등)
}

// Option: 옵션 함수 타입
type Option func(*Options)

// WithSchemaName: JSON schema 식별 이름을 설정합니다.
func WithSchemaName(name string) Option {
	return func(o *Options) {
		if name != "" {
			o.SchemaName = name
		}
	}
}

// WithTemperature: 생성 temperature를 설정합니다 (0보다 클 때만 적용).
func WithTemperature(temp float64) Option {
	return func(o *Options) {
		if temp > 0 {
			o.Temperature = &temp
		}
	}
}

// WithWebSearch: WebSearch tool 포함 여부를 설정합니다.
func WithWebSearch(enabled bool) Option {
	return func(o *Options) {
		o.WebSearch = &enabled
	}
}

// WithChatCompletions: Chat Completions API 모드를 활성화합니다 (cliproxy 호환).
func WithChatCompletions() Option {
	return func(o *Options) {
		o.ChatCompletions = true
	}
}

// WithReasoningEffort: reasoning 모델의 사고 깊이를 설정합니다 (high, xhigh 등).
func WithReasoningEffort(effort string) Option {
	return func(o *Options) {
		if effort != "" {
			o.ReasoningEffort = effort
		}
	}
}

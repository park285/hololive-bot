package llm

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/outputguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/guardrail"
)

type GuardedClient struct {
	client Client
	guard  *outputguard.Guard
}

func NewGuardedClient(client Client, guard *outputguard.Guard) *GuardedClient {
	return &GuardedClient{client: client, guard: guard}
}

func (c *GuardedClient) GenerateJSON(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any) (string, error) {
	if c == nil || c.client == nil {
		return "", errors.New("generate guarded json: llm client is unavailable")
	}

	text, err := c.client.GenerateJSON(ctx, systemPrompt, userPrompt, schema)
	if err != nil {
		return "", fmt.Errorf("generate JSON: %w", err)
	}

	if err := c.validateGeneratedJSON(systemPrompt, text); err != nil {
		return "", fmt.Errorf("validate generated JSON: %w", err)
	}

	return text, nil
}

func (c *GuardedClient) validateGeneratedJSON(systemPrompt, text string) error {
	if c.guard == nil {
		return fmt.Errorf("validate generated json: %w", guardrail.ErrOutputGuardUnavailable)
	}

	if err := validateBoundGeneratedOutput(c.guard, systemPrompt, text); err != nil {
		return fmt.Errorf("validate bound generated output: %w", err)
	}

	var value any

	if err := jsonv2.Unmarshal([]byte(text), &value); err != nil {
		return fmt.Errorf("decode generated json for output guard: %w", err)
	}

	if err := validateGeneratedValue(c.guard, value); err != nil {
		return fmt.Errorf("validate generated value: %w", err)
	}

	return nil
}

func validateBoundGeneratedOutput(guard *outputguard.Guard, systemPrompt, text string) error {
	bound, err := guard.Bind([]string{systemPrompt})
	if err != nil {
		return fmt.Errorf("bind generated output guard: %w", err)
	}

	evaluation := bound.Check(text)
	if evaluation.Decision != outputguard.DecisionBlock {
		return nil
	}

	guardrail.RecordBlock("output", "provider_response")

	//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
	return generatedOutputError(evaluation.Decision, evaluation.ReasonCodes, evaluation.RuleIDs, outputguard.ErrRestrictedGeneratedText)
}

func validateGeneratedValue(guard *outputguard.Guard, value any) error {
	var (
		err       error
		operation string
	)

	switch typed := value.(type) {
	case string:
		operation = "validate generated text"
		err = validateGeneratedText(guard, typed)
	case map[string]any:
		operation = "validate generated items"
		err = validateGeneratedItems(guard, objectValues(typed))
	case []any:
		operation = "validate generated items"
		err = validateGeneratedItems(guard, typed)
	}

	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}

	return nil
}

func validateGeneratedText(guard *outputguard.Guard, text string) error {
	evaluation, err := guardrail.ValidateGeneratedOutput(guard, text, "provider_response")
	if err == nil {
		return nil
	}

	//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
	return generatedOutputError(evaluation.Decision, evaluation.ReasonCodes, evaluation.RuleIDs, err)
}

func validateGeneratedItems(guard *outputguard.Guard, items []any) error {
	for _, item := range items {
		if err := validateGeneratedValue(guard, item); err != nil {
			return fmt.Errorf("validate generated value: %w", err)
		}
	}

	return nil
}

func objectValues(object map[string]any) []any {
	values := make([]any, 0, len(object))
	for _, value := range object {
		values = append(values, value)
	}

	return values
}

func generatedOutputError(decision outputguard.Decision, reasons []outputguard.ReasonCode, rules []string, err error) error {
	return fmt.Errorf("validate generated json: decision=%s reasons=%v rules=%v: %w", decision, reasons, rules, err)
}

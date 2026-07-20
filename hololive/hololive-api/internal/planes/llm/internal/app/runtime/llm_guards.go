package runtime

import (
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/pkg/outputguard"
	"github.com/park285/shared-go/pkg/promptguard"
)

type llmGuards struct {
	prompt *promptguard.Guard
	output *outputguard.Guard
}

func buildLLMGuards(logger *slog.Logger) (*llmGuards, error) {
	if logger == nil {
		logger = slog.Default()
	}
	prompt, err := promptguard.NewGuard(promptguard.Config{
		Enabled:             true,
		UseEmbeddedDefaults: true,
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("create llm prompt guard: %w", err)
	}
	if prompt.PolicyDigest() == "" {
		return nil, fmt.Errorf("create llm prompt guard: empty effective policy digest")
	}

	logger.Info("LLM guards configured",
		slog.String("prompt_policy_digest", prompt.PolicyDigest()),
		slog.String("rulepack_source", "embedded"),
	)
	return &llmGuards{prompt: prompt, output: outputguard.NewGuard()}, nil
}

package summarizer

import (
	"errors"
	"testing"
)

func TestConsensusDoesNotReviewDeterministicFallbackAfterPrimaryLLMFailure(t *testing.T) {
	validator := mustValidatorWithAllowlist(t)
	primary := NewSummarizer(&fakeLLM{err: errors.New("llm down")}, nil, validator, nil)
	reviewer := &fakeLLMWithCounter{response: approvedVerdictJSON(0.99)}

	cs := NewConsensusSummarizer(primary, reviewer, nil, validator, defaultConsensusConfig(), nil)

	if _, err := cs.Summarize(t.Context(), defaultTestInput()); err == nil {
		t.Fatal("Summarize() error = nil, want primary LLM failure for the service fallback owner")
	}

	if got := reviewer.callCount.Load(); got != 0 {
		t.Fatalf("reviewer calls = %d, want 0", got)
	}
}

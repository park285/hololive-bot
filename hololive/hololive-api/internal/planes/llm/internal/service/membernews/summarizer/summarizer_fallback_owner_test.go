package summarizer

import (
	"errors"
	"testing"
)

// primary LLM이 실패하면 summarizer는 결정적 digest를 스스로 만들지 않고 오류를 돌려준다. 결정적 fallback은 service가
// 한 곳에서 소유한다. 그렇지 않으면 consensus reviewer가 결정적 fallback digest를 LLM으로 다시 검토한다.
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

package polling

import "testing"

func TestValidateYouTubeProducerRuntimeBudgetRequiresLimiterForOversubscription(t *testing.T) {
	t.Parallel()

	summary := youtubeProducerBudgetSummary{CombinedRPM: 21, BudgetRPM: 20}
	if err := validateYouTubeProducerRuntimeBudget(summary, false); err == nil {
		t.Fatal("oversubscribed runtime without limiter was accepted")
	}
	if err := validateYouTubeProducerRuntimeBudget(summary, true); err != nil {
		t.Fatalf("oversubscribed runtime with limiter error = %v", err)
	}
}

func TestValidateYouTubeProducerRuntimeBudgetAcceptsInBudgetWithoutLimiter(t *testing.T) {
	t.Parallel()

	summary := youtubeProducerBudgetSummary{CombinedRPM: 20, BudgetRPM: 20}
	if err := validateYouTubeProducerRuntimeBudget(summary, false); err != nil {
		t.Fatalf("in-budget runtime error = %v", err)
	}
}

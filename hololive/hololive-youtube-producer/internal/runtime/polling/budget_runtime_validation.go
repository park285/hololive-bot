package polling

// validateYouTubeProducerRuntimeBudget keeps the static budget error fail-closed
// unless the concrete scraper client confirms that every request is admitted by
// a shared runtime rate limiter. Persisted user alarm targets may then
// oversubscribe scheduler demand without turning configuration into a startup
// kill switch; the limiter sheds the excess at execution time.
func validateYouTubeProducerRuntimeBudget(summary youtubeProducerBudgetSummary, limiterConfigured bool) error {
	err := validateYouTubeProducerPollerBudget(summary)
	if err != nil && limiterConfigured {
		return nil
	}
	return err
}

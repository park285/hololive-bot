package app

const (
	EventLLMRuntimeStarted  = "llm_scheduler.runtime.started"
	EventLLMRuntimeStopping = "llm_scheduler.runtime.stopping"
	EventLLMSchedulerStarted = "llm_scheduler.scheduler.started"

	EventLLMPromptBuilt             = "llm.prompt.built"
	EventLLMProviderRequestStarted  = "llm.provider.request.started"
	EventLLMProviderRequestSucceeded = "llm.provider.request.succeeded"
	EventLLMProviderRequestFailed   = "llm.provider.request.failed"
	EventLLMResultValidated         = "llm.result.validated"
	EventLLMResultValidationFailed  = "llm.result.validation.failed"
	EventLLMIntentWriteSucceeded    = "llm.notification_intent.write.succeeded"
	EventLLMIntentWriteFailed       = "llm.notification_intent.write.failed"
)

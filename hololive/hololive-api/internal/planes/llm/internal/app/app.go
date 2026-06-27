package app

import runtime "github.com/kapu/hololive-api/internal/planes/llm/internal/app/internal/runtime"

type DeliveryModule = runtime.DeliveryModule
type LLMSchedulerRuntime = runtime.LLMSchedulerRuntime
type UIEmoji = runtime.UIEmoji

const (
	EventLLMRuntimeStarted   = runtime.EventLLMRuntimeStarted
	EventLLMRuntimeStopping  = runtime.EventLLMRuntimeStopping
	EventLLMSchedulerStarted = runtime.EventLLMSchedulerStarted

	EventLLMPromptBuilt              = runtime.EventLLMPromptBuilt
	EventLLMProviderRequestStarted   = runtime.EventLLMProviderRequestStarted
	EventLLMProviderRequestSucceeded = runtime.EventLLMProviderRequestSucceeded
	EventLLMProviderRequestFailed    = runtime.EventLLMProviderRequestFailed
	EventLLMResultValidated          = runtime.EventLLMResultValidated
	EventLLMResultValidationFailed   = runtime.EventLLMResultValidationFailed
	EventLLMIntentWriteSucceeded     = runtime.EventLLMIntentWriteSucceeded
	EventLLMIntentWriteFailed        = runtime.EventLLMIntentWriteFailed
)

var DefaultEmoji = runtime.DefaultEmoji

var ProvideMajorEventAdjudicatorClient = runtime.ProvideMajorEventAdjudicatorClient
var ProvideMajorEventLLMClient = runtime.ProvideMajorEventLLMClient
var ProvideMajorEventReviewerClient = runtime.ProvideMajorEventReviewerClient
var ProvideMemberNewsAdjudicatorClient = runtime.ProvideMemberNewsAdjudicatorClient
var ProvideMemberNewsLLMClient = runtime.ProvideMemberNewsLLMClient
var ProvideMemberNewsReviewerClient = runtime.ProvideMemberNewsReviewerClient
var BuildDeliveryModule = runtime.BuildDeliveryModule
var BuildLLMSchedulerRuntime = runtime.BuildLLMSchedulerRuntime

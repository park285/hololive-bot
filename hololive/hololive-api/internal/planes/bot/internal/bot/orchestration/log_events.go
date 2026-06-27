package orchestration

const (
	EventBotMessageSkipped             = "bot.message.skipped"
	EventBotCommandReceived            = "bot.command.received"
	EventBotCommandUnknown             = "bot.command.unknown"
	EventBotCommandExecuteStarted      = "bot.command.execute.started"
	EventBotCommandExecuteSucceeded    = "bot.command.execute.succeeded"
	EventBotCommandExecuteFailed       = "bot.command.execute.failed"
	EventBotCommandAsyncRejected       = "bot.command.async.rejected"
	EventBotCommandPanic               = "bot.command.panic"
	EventBotCommandErrorResponseFailed = "bot.command.error_response.failed"

	EventBotLifecycleStarting          = "bot.lifecycle.starting"
	EventBotLifecycleStarted           = "bot.lifecycle.started"
	EventBotLifecycleShutdownRequested = "bot.lifecycle.shutdown.requested"
	EventBotLifecycleShutdownCompleted = "bot.lifecycle.shutdown.completed"
)

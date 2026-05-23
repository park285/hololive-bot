package bot

import (
	orchestration "github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration"
	"github.com/kapu/hololive-kakao-bot-go/internal/bot/internal/orchestration/orchcmd"
)

type MessageIngress = orchestration.MessageIngress

type Bot = orchestration.Bot

type CommandTransport = orchestration.CommandTransport

type Dependencies = orchestration.Dependencies

type CommandBuilder = orchcmd.CommandBuilder

type BotLifecycle = orchestration.BotLifecycle

type CommandRouter = orchcmd.CommandRouter

const (
	EventBotMessageSkipped             = orchestration.EventBotMessageSkipped
	EventBotCommandReceived            = orchestration.EventBotCommandReceived
	EventBotCommandUnknown             = orchestration.EventBotCommandUnknown
	EventBotCommandExecuteStarted      = orchestration.EventBotCommandExecuteStarted
	EventBotCommandExecuteSucceeded    = orchestration.EventBotCommandExecuteSucceeded
	EventBotCommandExecuteFailed       = orchestration.EventBotCommandExecuteFailed
	EventBotCommandAsyncRejected       = orchestration.EventBotCommandAsyncRejected
	EventBotCommandPanic               = orchestration.EventBotCommandPanic
	EventBotCommandErrorResponseFailed = orchestration.EventBotCommandErrorResponseFailed

	EventBotLifecycleStarting          = orchestration.EventBotLifecycleStarting
	EventBotLifecycleStarted           = orchestration.EventBotLifecycleStarted
	EventBotLifecycleShutdownRequested = orchestration.EventBotLifecycleShutdownRequested
	EventBotLifecycleShutdownCompleted = orchestration.EventBotLifecycleShutdownCompleted
)

var NewMessageIngress = orchestration.NewMessageIngress
var NewBot = orchestration.NewBot
var CloneCommandBuilders = orchcmd.CloneCommandBuilders
var NewCommandTransport = orchestration.NewCommandTransport
var NewBotLifecycle = orchestration.NewBotLifecycle
var NewCommandRouter = orchcmd.NewCommandRouter

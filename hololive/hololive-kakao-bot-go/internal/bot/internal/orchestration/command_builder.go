package orchestration

import "github.com/kapu/hololive-kakao-bot-go/internal/command"

type CommandBuilder func(deps *command.Dependencies) command.Command

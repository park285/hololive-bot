package orchcmd

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command"

type CommandBuilder func(deps *command.Dependencies) command.Command

package orchcmd

import "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"

type CommandBuilder func(deps *handlercore.Dependencies) handlercore.Command

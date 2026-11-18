package handlercore

import (
	"errors"
	"log/slog"
)

type BaseCommand struct {
	deps *Dependencies
}

func NewBaseCommand(deps *Dependencies) BaseCommand {
	return BaseCommand{deps: deps}
}

func (b *BaseCommand) EnsureBaseDeps() error {
	if b == nil || b.deps == nil {
		return errors.New("command dependencies not configured")
	}

	if b.deps.SendMessage == nil || b.deps.SendError == nil {
		return errors.New("message callbacks not configured")
	}

	if b.deps.Logger == nil {
		b.deps.Logger = slog.Default()
	}

	return nil
}

func (b *BaseCommand) Deps() *Dependencies {
	return b.deps
}

package command

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type factoryStubCommand struct {
	name string
}

func (s *factoryStubCommand) Name() string {
	return s.name
}

func (s *factoryStubCommand) Description() string {
	return "stub"
}

func (s *factoryStubCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	return nil
}

func TestBuildCommandsSkipsNilEntries(t *testing.T) {
	t.Parallel()

	commands := BuildCommands(
		nil,
		nil,
		func(*Dependencies) Command { return nil },
		func(*Dependencies) Command { return &factoryStubCommand{name: "ok"} },
	)
	if len(commands) != 1 {
		t.Fatalf("len(commands) = %d, want 1", len(commands))
	}
	if commands[0].Name() != "ok" {
		t.Fatalf("commands[0].Name() = %q, want %q", commands[0].Name(), "ok")
	}
}

func TestDefaultFactoriesCount(t *testing.T) {
	t.Parallel()

	if got, want := len(DefaultFactories()), 8; got != want {
		t.Fatalf("len(DefaultFactories()) = %d, want %d", got, want)
	}
}

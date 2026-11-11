package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlers "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

type recordingCommand struct {
	name  string
	err   error
	calls int
}

func (c *recordingCommand) Name() string        { return c.name }
func (c *recordingCommand) Description() string { return c.name }

func (c *recordingCommand) Execute(context.Context, *botcommand.CommandContext, map[string]any) error {
	c.calls++

	return c.err
}

func TestRegistryExecuteUnknownKeyReturnsErrUnknownCommand(t *testing.T) {
	registry := handlers.NewRegistry()

	err := registry.Execute(t.Context(), &botcommand.CommandContext{}, "missing", nil)
	if err == nil {
		t.Fatal("expected error for unregistered command, got nil")
	}

	if !errors.Is(err, handlers.ErrUnknownCommand) {
		t.Fatalf("error = %v, want ErrUnknownCommand in chain", err)
	}
}

func TestRegistryExecuteNormalizesRegisteredCommandName(t *testing.T) {
	registry := handlers.NewRegistry()
	handler := &recordingCommand{name: " Help "}
	registry.Register(handler)

	if got := registry.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}

	for _, key := range []string{"help", "HELP", " help "} {
		if err := registry.Execute(t.Context(), &botcommand.CommandContext{}, key, nil); err != nil {
			t.Fatalf("Execute(%q) error = %v", key, err)
		}
	}

	if handler.calls != 3 {
		t.Fatalf("handler calls = %d, want 3", handler.calls)
	}
}

func TestRegistryExecuteWrapsHandlerFailure(t *testing.T) {
	sentinel := errors.New("handler exploded")
	registry := handlers.NewRegistry()
	registry.Register(&recordingCommand{name: "live", err: sentinel})

	err := registry.Execute(t.Context(), &botcommand.CommandContext{}, "live", nil)
	if err == nil {
		t.Fatal("expected wrapped handler error, got nil")
	}

	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want handler sentinel in chain", err)
	}

	if !strings.Contains(err.Error(), "command live") {
		t.Fatalf("error = %q, want command name in wrapping", err)
	}
}

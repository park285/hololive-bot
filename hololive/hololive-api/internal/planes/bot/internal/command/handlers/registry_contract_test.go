package handlers_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlers "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

type recordingCommand struct {
	name  string
	err   error
	calls int
}

func (c *recordingCommand) Name() string        { return c.name }
func (c *recordingCommand) Description() string { return c.name }

func (c *recordingCommand) Dispatch(context.Context, *botcommand.CommandContext, botcommand.Params) error {
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

type liveRecorder struct {
	got []botcommand.LiveParams
}

func (c *liveRecorder) Name() string        { return "live" }
func (c *liveRecorder) Description() string { return "live" }

func (c *liveRecorder) Execute(_ context.Context, _ *botcommand.CommandContext, params botcommand.LiveParams) error {
	c.got = append(c.got, params)

	return nil
}

func TestRegistryDispatchesTypedParams(t *testing.T) {
	handler := &liveRecorder{}
	registry := handlers.NewRegistry()
	registry.Register(handlercore.Bind(handler))

	for _, params := range []botcommand.Params{botcommand.LiveParams{Member: "미코"}, nil} {
		if err := registry.Execute(t.Context(), &botcommand.CommandContext{}, "live", params); err != nil {
			t.Fatalf("Execute(%#v) error = %v", params, err)
		}
	}

	if want := []botcommand.LiveParams{{Member: "미코"}, {}}; !slices.Equal(handler.got, want) {
		t.Fatalf("handler params = %#v, want %#v", handler.got, want)
	}

	err := registry.Execute(t.Context(), &botcommand.CommandContext{}, "live", botcommand.UpcomingParams{Member: "미코"})
	if !errors.Is(err, handlercore.ErrParamsMismatch) || len(handler.got) != 2 {
		t.Fatalf("mismatched params error = %v, handler calls = %d", err, len(handler.got))
	}
}

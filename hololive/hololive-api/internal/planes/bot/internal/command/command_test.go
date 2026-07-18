package command_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

type recordingCommand struct {
	name    string
	err     error
	calls   int
	mutate  bool
	gotKeys []string
}

func (c *recordingCommand) Name() string        { return c.name }
func (c *recordingCommand) Description() string { return c.name }

func (c *recordingCommand) Execute(_ context.Context, _ *domain.CommandContext, params map[string]any) error {
	c.calls++
	for key := range params {
		c.gotKeys = append(c.gotKeys, key)
	}
	if c.mutate {
		params["mutated"] = true
	}
	return c.err
}

func TestRegistryExecuteUnknownKeyReturnsErrUnknownCommand(t *testing.T) {
	registry := command.NewRegistry()

	err := registry.Execute(context.Background(), &domain.CommandContext{}, "missing", nil)
	if err == nil {
		t.Fatal("expected error for unregistered command, got nil")
	}
	if !errors.Is(err, command.ErrUnknownCommand) {
		t.Fatalf("error = %v, want ErrUnknownCommand in chain", err)
	}
}

func TestRegistryExecuteNormalizesRegisteredCommandName(t *testing.T) {
	registry := command.NewRegistry()
	handler := &recordingCommand{name: " Help "}
	registry.Register(handler)

	if got := registry.Count(); got != 1 {
		t.Fatalf("Count() = %d, want 1", got)
	}

	for _, key := range []string{"help", "HELP", " help "} {
		if err := registry.Execute(context.Background(), &domain.CommandContext{}, key, nil); err != nil {
			t.Fatalf("Execute(%q) error = %v", key, err)
		}
	}
	if handler.calls != 3 {
		t.Fatalf("handler calls = %d, want 3", handler.calls)
	}
}

func TestRegistryExecuteWrapsHandlerFailure(t *testing.T) {
	sentinel := errors.New("handler exploded")
	registry := command.NewRegistry()
	registry.Register(&recordingCommand{name: "live", err: sentinel})

	err := registry.Execute(context.Background(), &domain.CommandContext{}, "live", nil)
	if err == nil {
		t.Fatal("expected wrapped handler error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want handler sentinel in chain", err)
	}
	if !strings.Contains(err.Error(), "failed to execute command live") {
		t.Fatalf("error = %q, want command name in wrapping", err)
	}
}

func passthroughNormalize(cmdType domain.CommandType, params map[string]any) (key string, normalized map[string]any) {
	return string(cmdType), params
}

func TestSequentialDispatcherPublishSkipsUnknownEvents(t *testing.T) {
	registry := command.NewRegistry()
	help := &recordingCommand{name: "help"}
	registry.Register(help)

	dispatcher := command.NewSequentialDispatcher(registry, passthroughNormalize)
	executed, err := dispatcher.Publish(context.Background(), &domain.CommandContext{},
		command.Event{Type: domain.CommandUnknown},
		command.Event{Type: domain.CommandHelp},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}
	if help.calls != 1 {
		t.Fatalf("help handler calls = %d, want 1", help.calls)
	}
}

func TestSequentialDispatcherPublishStopsAtFirstFailure(t *testing.T) {
	sentinel := errors.New("first handler failed")
	registry := command.NewRegistry()
	failing := &recordingCommand{name: "first", err: sentinel}
	untouched := &recordingCommand{name: "second"}
	registry.Register(failing)
	registry.Register(untouched)

	normalize := func(_ domain.CommandType, params map[string]any) (key string, normalized map[string]any) {
		key, ok := params["key"].(string)
		if !ok {
			return "", params
		}
		return key, params
	}

	dispatcher := command.NewSequentialDispatcher(registry, normalize)
	executed, err := dispatcher.Publish(context.Background(), &domain.CommandContext{},
		command.Event{Type: domain.CommandHelp, Params: map[string]any{"key": "first"}},
		command.Event{Type: domain.CommandHelp, Params: map[string]any{"key": "second"}},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want first handler sentinel in chain", err)
	}
	if executed != 0 {
		t.Fatalf("executed = %d, want 0", executed)
	}
	if untouched.calls != 0 {
		t.Fatalf("second handler calls = %d, want 0", untouched.calls)
	}
}

func TestSequentialDispatcherPublishClonesEventParams(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(&recordingCommand{name: "help", mutate: true})

	original := map[string]any{"member": "미즈미야"}
	dispatcher := command.NewSequentialDispatcher(registry, passthroughNormalize)
	if _, err := dispatcher.Publish(context.Background(), &domain.CommandContext{},
		command.Event{Type: domain.CommandHelp, Params: original},
	); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if _, leaked := original["mutated"]; leaked {
		t.Fatal("handler mutation leaked into the original event params")
	}
	if len(original) != 1 {
		t.Fatalf("original params len = %d, want 1", len(original))
	}
}

func TestSequentialDispatcherWithoutNormalizeFuncFails(t *testing.T) {
	dispatcher := command.NewSequentialDispatcher(command.NewRegistry(), nil)

	executed, err := dispatcher.Publish(context.Background(), &domain.CommandContext{},
		command.Event{Type: domain.CommandHelp},
	)
	if err == nil {
		t.Fatal("expected configuration error, got nil")
	}
	if !strings.Contains(err.Error(), "dispatcher not configured") {
		t.Fatalf("error = %q, want dispatcher not configured", err)
	}
	if executed != 0 {
		t.Fatalf("executed = %d, want 0", executed)
	}
}

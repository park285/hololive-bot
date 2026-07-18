package bot_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	bot "github.com/kapu/hololive-api/internal/planes/bot/internal/bot"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

type stubBotCommand struct {
	name  string
	calls int
}

func (c *stubBotCommand) Name() string        { return c.name }
func (c *stubBotCommand) Description() string { return c.name }

func (c *stubBotCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	c.calls++
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCloneCommandBuildersNilSourceReturnsNil(t *testing.T) {
	if got := bot.CloneCommandBuilders(nil); got != nil {
		t.Fatalf("CloneCommandBuilders(nil) = %v, want nil", got)
	}
}

func TestCloneCommandBuildersProducesIndependentSlice(t *testing.T) {
	first := &stubBotCommand{name: "first"}
	second := &stubBotCommand{name: "second"}
	src := []bot.CommandBuilder{
		func(*command.Dependencies) command.Command { return first },
		func(*command.Dependencies) command.Command { return second },
	}

	clone := bot.CloneCommandBuilders(src)
	if len(clone) != len(src) {
		t.Fatalf("clone len = %d, want %d", len(clone), len(src))
	}
	if got := clone[1](nil); got != command.Command(second) {
		t.Fatalf("clone[1]() = %v, want the original second builder result", got)
	}

	clone[0] = func(*command.Dependencies) command.Command { return second }
	if got := src[0](nil); got != command.Command(first) {
		t.Fatal("mutating the clone changed the source builders")
	}
}

func TestCommandRouterExecuteWithoutRegistryFails(t *testing.T) {
	router := bot.NewCommandRouter(nil, discardLogger(), nil, nil, nil)

	err := router.Execute(context.Background(), &domain.CommandContext{Room: "room-1"}, domain.CommandHelp, nil)
	if err == nil {
		t.Fatal("expected error for missing registry, got nil")
	}
	if !strings.Contains(err.Error(), "command registry is not initialized") {
		t.Fatalf("error = %q, want registry initialization message", err)
	}
}

func TestCommandRouterExecutesRegisteredCommand(t *testing.T) {
	registry := command.NewRegistry()
	router := bot.NewCommandRouter(registry, discardLogger(), nil, nil, nil)

	key, _ := router.NormalizeCommand(domain.CommandHelp, nil)
	if key == "" {
		t.Fatal("NormalizeCommand returned empty key for CommandHelp")
	}

	stub := &stubBotCommand{name: key}
	registry.Register(stub)

	err := router.Execute(context.Background(), &domain.CommandContext{Room: "room-1", UserID: "user-1"}, domain.CommandHelp, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("registered command calls = %d, want 1", stub.calls)
	}
}

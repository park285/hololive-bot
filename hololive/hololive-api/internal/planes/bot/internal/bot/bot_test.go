package bot_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlers "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

type stubBotCommand struct {
	name  string
	calls int
}

func (c *stubBotCommand) Name() string        { return c.name }
func (c *stubBotCommand) Description() string { return c.name }

func (c *stubBotCommand) Execute(context.Context, *botcommand.CommandContext, map[string]any) error {
	c.calls++
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestCommandRouterExecuteWithoutRegistryFails(t *testing.T) {
	router := orchcmd.NewCommandRouter(nil, discardLogger(), nil, nil, nil)

	err := router.Execute(t.Context(), &botcommand.CommandContext{Room: "room-1"}, botcommand.CommandHelp, nil)
	if err == nil {
		t.Fatal("expected error for missing registry, got nil")
	}

	if !strings.Contains(err.Error(), "command registry is not initialized") {
		t.Fatalf("error = %q, want registry initialization message", err)
	}
}

func TestCommandRouterExecutesRegisteredCommand(t *testing.T) {
	registry := handlers.NewRegistry()
	router := orchcmd.NewCommandRouter(registry, discardLogger(), nil, nil, nil)

	key, _ := router.NormalizeCommand(botcommand.CommandHelp, nil)
	if key == "" {
		t.Fatal("NormalizeCommand returned empty key for CommandHelp")
	}

	stub := &stubBotCommand{name: key}
	registry.Register(stub)

	err := router.Execute(t.Context(), &botcommand.CommandContext{Room: "room-1", UserID: "user-1"}, botcommand.CommandHelp, nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if stub.calls != 1 {
		t.Fatalf("registered command calls = %d, want 1", stub.calls)
	}
}

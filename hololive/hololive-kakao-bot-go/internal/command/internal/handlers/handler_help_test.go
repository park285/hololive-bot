package handlers

import (
	"context"
	"log/slog"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"gorm.io/gorm"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
)

func setupHelpTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&domain.NotificationTemplate{}); err != nil {
		t.Fatalf("migrate template table: %v", err)
	}

	if err := db.Create([]domain.NotificationTemplate{
		{
			TemplateKey: domain.TemplateKeyCmdHelp,
			Body:        "도움말\n명령어: {{.Prefix}}도움말",
		},
	}).Error; err != nil {
		t.Fatalf("seed help template: %v", err)
	}

	return serviceTemplate.NewRenderer(db, slog.New(slog.DiscardHandler))
}

func TestHelpCommand_Name(t *testing.T) {
	cmd := NewHelpCommand(nil)
	if cmd.Name() != "help" {
		t.Fatalf("Name() = %q, want %q", cmd.Name(), "help")
	}
}

func TestHelpCommand_Description(t *testing.T) {
	cmd := NewHelpCommand(nil)
	if cmd.Description() == "" {
		t.Fatal("Description() should not be empty")
	}
}

func TestHelpCommand_Execute_GoldenPath(t *testing.T) {
	var sentRoom, sentMessage string

	deps := &Dependencies{
		Formatter: adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		SendMessage: func(_ context.Context, room, message string) error {
			sentRoom = room
			sentMessage = message
			return nil
		},
		SendError: func(_ context.Context, _, _ string) error { return nil },
		Logger:    slog.New(slog.DiscardHandler),
	}

	cmd := NewHelpCommand(deps)
	cmdCtx := &domain.CommandContext{Room: "room-1"}

	if err := cmd.Execute(t.Context(), cmdCtx, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if sentRoom != "room-1" {
		t.Fatalf("sent to room %q, want %q", sentRoom, "room-1")
	}

	if sentMessage == "" {
		t.Fatal("expected non-empty help message")
	}
}

func TestHelpCommand_Execute_NilDeps(t *testing.T) {
	cmd := NewHelpCommand(nil)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestHelpCommand_Execute_NilSendMessage(t *testing.T) {
	deps := &Dependencies{
		Formatter: adapter.NewResponseFormatter("!", nil),
	}
	cmd := NewHelpCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil SendMessage")
	}
}

func TestHelpCommand_Execute_NilFormatter(t *testing.T) {
	deps := &Dependencies{
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
	}
	cmd := NewHelpCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil Formatter")
	}
}

func TestHelpCommand_Execute_NilReceiver(t *testing.T) {
	var cmd *HelpCommand

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

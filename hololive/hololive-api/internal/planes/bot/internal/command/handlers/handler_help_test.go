package handlers

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
)

func setupHelpTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`); err != nil {
		t.Fatalf("clear templates: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, body)
		VALUES ($1, NULL, $2)
		ON CONFLICT (template_key) WHERE channel_id IS NULL
		DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
	`, domain.TemplateKeyCmdHelp, "도움말\n명령어: {{.Prefix}}도움말"); err != nil {
		t.Fatalf("seed help template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler))
}

type stubHelpImageRenderer struct {
	images [][]byte
	err    error
	calls  int
	text   string
}

func (s *stubHelpImageRenderer) RenderHelpImages(_ context.Context, text string) ([][]byte, error) {
	s.calls++
	s.text = text
	cloned := make([][]byte, len(s.images))
	for index, imageData := range s.images {
		cloned[index] = bytes.Clone(imageData)
	}
	return cloned, s.err
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

func TestHelpCommand_Execute_GoldenPathSendsImagesInOrder(t *testing.T) {
	renderer := &stubHelpImageRenderer{images: [][]byte{[]byte("one"), []byte("two"), []byte("three")}}
	var sent [][]byte
	textCalls := 0

	deps := &Dependencies{
		Formatter:         adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageRenderer: renderer,
		SendMessage: func(_ context.Context, _, _ string) error {
			textCalls++
			return nil
		},
		SendImage: func(_ context.Context, room string, imageData []byte, _ ...iris.SendOption) error {
			if room != "room-1" {
				t.Fatalf("image room = %q, want room-1", room)
			}
			sent = append(sent, bytes.Clone(imageData))
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewHelpCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(sent) != 3 || string(sent[0]) != "one" || string(sent[1]) != "two" || string(sent[2]) != "three" {
		t.Fatalf("sent images = %q", sent)
	}
	if textCalls != 0 {
		t.Fatalf("text fallback calls = %d, want 0", textCalls)
	}
	if renderer.calls != 1 || !strings.Contains(renderer.text, "명령어: !도움말") {
		t.Fatalf("renderer calls=%d text=%q", renderer.calls, renderer.text)
	}
}

func TestHelpCommand_Execute_RenderFailureFallsBackToText(t *testing.T) {
	renderErr := errors.New("render failed")
	var fallback string
	deps := &Dependencies{
		Formatter:         adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageRenderer: &stubHelpImageRenderer{err: renderErr},
		SendMessage: func(_ context.Context, _ string, message string) error {
			fallback = message
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			t.Fatal("SendImage must not be called after render failure")
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewHelpCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(fallback, "명령어: !도움말") {
		t.Fatalf("fallback = %q", fallback)
	}
}

func TestHelpCommand_Execute_PartialImageFailureFallsBackToText(t *testing.T) {
	imageErr := errors.New("image failed")
	var fallback string
	imageCalls := 0
	deps := &Dependencies{
		Formatter: adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageRenderer: &stubHelpImageRenderer{
			images: [][]byte{[]byte("one"), []byte("two"), []byte("three")},
		},
		SendMessage: func(_ context.Context, _ string, message string) error {
			fallback = message
			return nil
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			imageCalls++
			if imageCalls == 2 {
				return imageErr
			}
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewHelpCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if imageCalls != 2 {
		t.Fatalf("image calls = %d, want 2", imageCalls)
	}
	if fallback == "" {
		t.Fatal("expected text fallback after partial image failure")
	}
}

func TestHelpCommand_Execute_JoinsImageAndTextFailures(t *testing.T) {
	imageErr := errors.New("image failed")
	textErr := errors.New("text failed")
	deps := &Dependencies{
		Formatter:         adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageRenderer: &stubHelpImageRenderer{images: [][]byte{[]byte("one")}},
		SendMessage: func(_ context.Context, _, _ string) error {
			return textErr
		},
		SendImage: func(_ context.Context, _ string, _ []byte, _ ...iris.SendOption) error {
			return imageErr
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewHelpCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil)
	if !errors.Is(err, imageErr) || !errors.Is(err, textErr) {
		t.Fatalf("Execute error = %v, want joined failures", err)
	}
}

func TestHelpCommand_Execute_MissingImageCapabilityUsesTextFallback(t *testing.T) {
	var sentMessage string
	deps := &Dependencies{
		Formatter: adapter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		SendMessage: func(_ context.Context, _ string, message string) error {
			sentMessage = message
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewHelpCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sentMessage == "" {
		t.Fatal("expected text fallback")
	}
}

func TestHelpCommand_Execute_NilDependencies(t *testing.T) {
	if err := NewHelpCommand(nil).Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err == nil {
		t.Fatal("expected error for nil deps")
	}
	if err := NewHelpCommand(&Dependencies{Formatter: adapter.NewResponseFormatter("!", nil)}).Execute(
		t.Context(),
		&domain.CommandContext{Room: "room-1"},
		nil,
	); err == nil {
		t.Fatal("expected error for nil SendMessage")
	}
	if err := NewHelpCommand(&Dependencies{SendMessage: func(_ context.Context, _, _ string) error { return nil }}).Execute(
		t.Context(),
		&domain.CommandContext{Room: "room-1"},
		nil,
	); err == nil {
		t.Fatal("expected error for nil Formatter")
	}
}

func TestHelpCommand_Execute_NilContexts(t *testing.T) {
	deps := &Dependencies{
		Formatter:   adapter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
	}
	if err := NewHelpCommand(deps).Execute(t.Context(), nil, nil); err == nil {
		t.Fatal("expected error for nil command context")
	}
	var cmd *HelpCommand
	if err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, nil); err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

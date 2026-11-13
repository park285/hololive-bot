package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

func setupHelpTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	pool := dbtest.NewPool(t)
	if _, err := pool.Exec(t.Context(), `DELETE FROM notification_templates`); err != nil {
		t.Fatalf("clear templates: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notification_templates(template_key, channel_id, format_variant, body)
		SELECT $1, NULL, variant, $2 FROM unnest(ARRAY['plain', 'richtext']) AS variant
		ON CONFLICT (template_key, format_variant) WHERE channel_id IS NULL
		DO UPDATE SET body = EXCLUDED.body, updated_at = NOW()
	`, domain.TemplateKeyCmdHelp, "도움말\n명령어: {{.Prefix}}도움말"); err != nil {
		t.Fatalf("seed help template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler))
}

type stubHelpImageProvider struct {
	images [][]byte
	err    error
	calls  int
}

func (s *stubHelpImageProvider) HelpImages(_ context.Context) ([][]byte, error) {
	s.calls++

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

func TestHelpCommand_Execute_GoldenPathSendsImageAlbum(t *testing.T) {
	provider := &stubHelpImageProvider{images: [][]byte{[]byte("one"), []byte("two"), []byte("three")}}

	var sent [][]byte

	albumCalls := 0
	textCalls := 0

	deps := &handlercore.Dependencies{
		Formatter:         formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageProvider: provider,
		SendMessage: func(_ context.Context, _, _ string) error {
			textCalls++
			return nil
		},
		SendImages: func(_ context.Context, room string, images [][]byte) error {
			if room != testRoomID {
				t.Fatalf("image room = %q, want room-1", room)
			}

			albumCalls++

			for _, imageData := range images {
				sent = append(sent, bytes.Clone(imageData))
			}

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	if err := NewHelpCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if len(sent) != 3 || string(sent[0]) != "one" || string(sent[1]) != "two" || string(sent[2]) != "three" {
		t.Fatalf("sent images = %q", sent)
	}

	if albumCalls != 1 {
		t.Fatalf("image album calls = %d, want 1", albumCalls)
	}

	if textCalls != 0 {
		t.Fatalf("text fallback calls = %d, want 0", textCalls)
	}

	if provider.calls != 1 {
		t.Fatalf("help image provider calls = %d, want 1", provider.calls)
	}
}

func TestHelpCommand_Execute_ImageProviderFailureFallsBackToText(t *testing.T) {
	providerErr := errors.New("load images failed")

	var fallback string

	deps := &handlercore.Dependencies{
		Formatter:         formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageProvider: &stubHelpImageProvider{err: providerErr},
		SendMessage: func(_ context.Context, _, message string) error {
			fallback = message
			return nil
		},
		SendImages: func(_ context.Context, _ string, _ [][]byte) error {
			t.Fatal("SendImages must not be called after image provider failure")

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	before := imageTextFallbackCount("help", imageTextFallbackReasonRenderFailed)

	if err := NewHelpCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if got := imageTextFallbackCount("help", imageTextFallbackReasonRenderFailed) - before; got != 1 {
		t.Fatalf("image text fallback delta = %v, want 1", got)
	}

	if !strings.Contains(fallback, "명령어: !도움말") {
		t.Fatalf("fallback = %q", fallback)
	}
}

func TestHelpCommand_Execute_AlbumFailureFallsBackToText(t *testing.T) {
	imageErr := errors.New("image failed")

	var fallback string

	imageCalls := 0
	deps := &handlercore.Dependencies{
		Formatter: formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageProvider: &stubHelpImageProvider{
			images: [][]byte{[]byte("one"), []byte("two"), []byte("three")},
		},
		SendMessage: func(_ context.Context, _, message string) error {
			fallback = message
			return nil
		},
		SendImages: func(_ context.Context, _ string, _ [][]byte) error {
			imageCalls++
			return imageErr
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	before := imageTextFallbackCount("help", imageTextFallbackReasonSendFailed)

	if err := NewHelpCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, nil); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if got := imageTextFallbackCount("help", imageTextFallbackReasonSendFailed) - before; got != 1 {
		t.Fatalf("image text fallback delta = %v, want 1", got)
	}

	if imageCalls != 1 {
		t.Fatalf("image album calls = %d, want 1", imageCalls)
	}

	if fallback == "" {
		t.Fatal("expected text fallback after image album failure")
	}
}

func TestHelpCommand_Execute_AlbumOutcomeUnknownSuppressesTextFallback(t *testing.T) {
	for _, imageErr := range []error{transport.ErrReplyOutcomeUnknown, transport.ErrReplyStagingFailed} {
		t.Run(imageErr.Error(), func(t *testing.T) {
			imageCalls, textCalls := 0, 0
			deps := &handlercore.Dependencies{
				Formatter:         formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
				HelpImageProvider: &stubHelpImageProvider{images: [][]byte{[]byte("one"), []byte("two")}},
				SendMessage:       func(context.Context, string, string) error { textCalls++; return nil },
				SendImages: func(context.Context, string, [][]byte) error {
					imageCalls++
					return fmt.Errorf("send help album: %w", imageErr)
				},
				Logger: slog.New(slog.DiscardHandler),
			}
			before := imageTextFallbackCount("help", imageTextFallbackReasonOutcomeUnknown)
			err := NewHelpCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, nil)

			if !errors.Is(err, imageErr) {
				t.Fatalf("Execute() error = %v, want preserved outcome uncertainty", err)
			}

			if imageCalls != 1 || textCalls != 0 {
				t.Fatalf("uncertain album: images=%d text=%d", imageCalls, textCalls)
			}

			if got := imageTextFallbackCount("help", imageTextFallbackReasonOutcomeUnknown) - before; got != 1 {
				t.Fatalf("image text fallback delta = %v, want 1", got)
			}
		})
	}
}

func TestHelpCommand_Execute_JoinsImageAndTextFailures(t *testing.T) {
	imageErr := errors.New("image failed")
	textErr := errors.New("text failed")
	deps := &handlercore.Dependencies{
		Formatter:         formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		HelpImageProvider: &stubHelpImageProvider{images: [][]byte{[]byte("one")}},
		SendMessage: func(_ context.Context, _, _ string) error {
			return textErr
		},
		SendImages: func(_ context.Context, _ string, _ [][]byte) error {
			return imageErr
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewHelpCommand(deps).Execute(t.Context(), &botcommand.CommandContext{Room: testRoomID}, nil)
	if !errors.Is(err, imageErr) || !errors.Is(err, textErr) {
		t.Fatalf("Execute error = %v, want joined failures", err)
	}
}

func TestHelpCommand_CheckDependencies_RequiresImageCapability(t *testing.T) {
	deps := &handlercore.Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", setupHelpTestRenderer(t)),
		SendMessage: func(context.Context, string, string) error { return nil },
		Logger:      slog.New(slog.DiscardHandler),
	}

	err := NewHelpCommand(deps).CheckDependencies()
	if err == nil || !strings.Contains(err.Error(), "help image capability not configured") {
		t.Fatalf("CheckDependencies error = %v, want help image capability not configured", err)
	}
}

func TestHelpCommand_CheckDependencies_RejectsMissingDependencies(t *testing.T) {
	if err := NewHelpCommand(nil).CheckDependencies(); err == nil || err.Error() != "help command dependencies not configured" {
		t.Fatalf("nil deps: CheckDependencies error = %v", err)
	}

	for name, tc := range map[string]struct {
		drop func(*handlercore.Dependencies)
		want string
	}{
		"complete":         {drop: func(*handlercore.Dependencies) {}},
		"nil send message": {drop: func(d *handlercore.Dependencies) { d.SendMessage = nil }, want: "message callback not configured"},
		"nil formatter":    {drop: func(d *handlercore.Dependencies) { d.Formatter = nil }, want: "formatter not configured"},
	} {
		deps := &handlercore.Dependencies{
			Formatter:         formatter.NewResponseFormatter("!", nil),
			SendMessage:       func(context.Context, string, string) error { return nil },
			HelpImageProvider: &stubHelpImageProvider{},
			SendImages:        func(context.Context, string, [][]byte) error { return nil },
		}
		tc.drop(deps)

		err := NewHelpCommand(deps).CheckDependencies()
		if got := fmt.Sprint(err); (tc.want == "" && err != nil) || (tc.want != "" && got != tc.want) {
			t.Fatalf("%s: CheckDependencies error = %v, want %q", name, err, tc.want)
		}
	}
}

func TestHelpCommand_Execute_NilCommandContext(t *testing.T) {
	deps := &handlercore.Dependencies{
		Formatter:   formatter.NewResponseFormatter("!", nil),
		SendMessage: func(_ context.Context, _, _ string) error { return nil },
	}
	if err := NewHelpCommand(deps).Execute(t.Context(), nil, nil); err == nil {
		t.Fatal("expected error for nil command context")
	}
}

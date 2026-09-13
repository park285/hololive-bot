package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/info"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
)

func setupProfileCommandTestRenderer(t *testing.T) *serviceTemplate.Renderer {
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
	`, domain.TemplateKeyCmdProfile, "👤 {{if len .Names}}{{index .Names 0}}{{else}}멤버 정보{{end}}"); err != nil {
		t.Fatalf("seed profile template: %v", err)
	}

	return serviceTemplate.NewRenderer(pool, slog.New(slog.DiscardHandler))
}

func TestMemberInfoCommand_Execute_SendsTextProfile(t *testing.T) {
	provider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-fubuki",
		Name:      "Shirakami Fubuki",
	}})

	var (
		textSent  string
		imageSent bool
	)

	deps := &handlercore.Dependencies{
		Matcher:     matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		MembersData: provider,
		Formatter:   formatter.NewResponseFormatter("!", setupProfileCommandTestRenderer(t)),
		SendMessage: func(_ context.Context, _, msg string) error {
			textSent = msg
			return nil
		},
		SendImage: func(context.Context, string, []byte, ...iris.SendOption) error {
			imageSent = true
			return nil
		},
		SendError: func(_ context.Context, _, msg string) error {
			t.Fatalf("unexpected SendError: %s", msg)

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := info.NewMemberInfoCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{
		paramMember: "Shirakami Fubuki",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(textSent, "시라카미 후부키") && !strings.Contains(textSent, "Shirakami Fubuki") {
		t.Fatalf("profile text = %q, want member name included", textSent)
	}

	if imageSent {
		t.Fatal("image path must not be used for profile")
	}
}

func TestMemberInfoCommandPrefersRequestedSharedChannelMember(t *testing.T) {
	provider := newContextAwareMemberProvider([]*domain.Member{
		{Name: "Izuki Michiru", ChannelID: "holoan-shared"},
		{Name: "holoAN", ChannelID: "holoan-shared"},
	})

	var textSent string

	deps := &handlercore.Dependencies{
		Matcher:     matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		MembersData: provider,
		Formatter:   formatter.NewResponseFormatter("!", setupProfileCommandTestRenderer(t)),
		SendMessage: func(_ context.Context, _, msg string) error { textSent = msg; return nil },

		SendError: func(_ context.Context, _, msg string) error {
			t.Fatalf("unexpected error: %s", msg)

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	for _, params := range []map[string]any{
		{"member": "Izuki Michiru", "channel_id": "holoan-shared"},
		{"query": "Izuki Michiru"},
		{"query": "Michiru"},
		{"query": "IZUKI  MICHIRU"},
	} {
		if err := info.NewMemberInfoCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, params); err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(textSent, "Izuki Michiru") {
			t.Fatalf("wrong shared-channel identity: %q", textSent)
		}
	}
}

type failedMemberInfoLoader struct{ domain.MemberDataProvider }

func (p failedMemberInfoLoader) LoadAllMembers() ([]*domain.Member, error) {
	return nil, errors.New("member DB unavailable")
}

func TestMemberInfoDoesNotTurnDatabaseFailureIntoNotFound(t *testing.T) {
	deps := &handlercore.Dependencies{
		MembersData: failedMemberInfoLoader{MemberDataProvider: newContextAwareMemberProvider(nil)},
		Formatter:   formatter.NewResponseFormatter("!", setupProfileCommandTestRenderer(t)),

		SendMessage: func(context.Context, string, string) error {
			t.Fatal("database error reported as message")

			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("database error reported as not-found")

			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	// WithContext도 error-aware loader 계약을 보존해야 한다.
	err := info.NewMemberInfoCommand(deps).Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, map[string]any{"query": "Michiru"})
	if err == nil || !strings.Contains(err.Error(), "member DB unavailable") {
		t.Fatalf("error=%v", err)
	}
}

func (p failedMemberInfoLoader) WithContext(context.Context) domain.MemberDataProvider { return p }

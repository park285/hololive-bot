package handlers

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/member"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/park285/iris-client-go/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
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
	profiles, err := member.NewProfileService(nil, provider, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewProfileService() error = %v", err)
	}

	var textSent string
	var imageSent bool
	deps := &Dependencies{
		Matcher:          matcher.NewMatcher(nilBaseContext(), provider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		MembersData:      provider,
		OfficialProfiles: profiles,
		Formatter:        adapter.NewResponseFormatter("!", setupProfileCommandTestRenderer(t)),
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

	err = NewMemberInfoCommand(deps).Execute(context.Background(), &domain.CommandContext{Room: "room-1"}, map[string]any{
		"member": "Shirakami Fubuki",
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

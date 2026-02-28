package command

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/notification"

	"log/slog"
)

func TestAlarmCommand_InvalidAction(t *testing.T) {
	var sentError string

	deps := &Dependencies{
		Alarm:     &notification.AlarmService{},
		Matcher:   &matcher.MemberMatcher{},
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(ctx context.Context, room, message string) error {
			return nil
		},
		SendError: func(ctx context.Context, room, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.Default(),
	}

	cmd := NewAlarmCommand(deps)
	params := map[string]any{
		"action":      "invalid",
		"sub_command": "설정123",
		"member":      "설정123",
	}

	ctx := &domain.CommandContext{
		Room:     "room-1",
		UserName: "user-1",
	}

	if err := cmd.Execute(context.Background(), ctx, params); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	expectedMessage := deps.Formatter.InvalidAlarmUsage()
	if sentError != expectedMessage {
		t.Fatalf("expected error message %q, got %q", expectedMessage, sentError)
	}
}

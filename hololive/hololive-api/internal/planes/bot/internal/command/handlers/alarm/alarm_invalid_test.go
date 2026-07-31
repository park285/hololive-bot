package alarm

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/privacylog"
)

const alarmPrivacySentinel = "SENTINEL"

type recordedAlarmError struct {
	room    string
	message string
}

func newInvalidAlarmCommand(t *testing.T) (command *AlarmCommand, sent *[]recordedAlarmError, logs *bytes.Buffer) {
	t.Helper()

	var (
		recorded []recordedAlarmError
		buffer   bytes.Buffer
	)

	deps := &handlercore.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
		SendError: func(_ context.Context, room, message string) error {
			recorded = append(recorded, recordedAlarmError{room: room, message: message})

			return nil
		},
	}

	return NewAlarmCommand(deps), &recorded, &buffer
}

func TestHandleInvalidAlwaysAnswersWithTheUsageError(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"nil params":     nil,
		"empty params":   {},
		"ignored params": {"sub_command": "오타-" + alarmPrivacySentinel, "member": "미코-" + alarmPrivacySentinel},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command, sent, _ := newInvalidAlarmCommand(t)
			cmdCtx := &domain.CommandContext{Room: "123456789", RoomName: "룸제목-" + alarmPrivacySentinel}

			if err := command.handleInvalid(t.Context(), cmdCtx, params); err != nil {
				t.Fatalf("handleInvalid() error = %v", err)
			}

			if len(*sent) != 1 {
				t.Fatalf("SendError calls = %d, want 1", len(*sent))
			}
			if (*sent)[0].room != cmdCtx.Room {
				t.Fatalf("SendError room = %q, want %q", (*sent)[0].room, cmdCtx.Room)
			}
			if (*sent)[0].message != messaging.ErrInvalidAlarmUsage {
				t.Fatalf("SendError message = %q, want %q", (*sent)[0].message, messaging.ErrInvalidAlarmUsage)
			}
		})
	}
}

func TestHandleInvalidLogKeepsRoomIDWithoutUserInput(t *testing.T) {
	t.Parallel()

	command, _, logs := newInvalidAlarmCommand(t)
	cmdCtx := &domain.CommandContext{
		Room:     "123456789",
		RoomName: "룸제목-" + alarmPrivacySentinel,
		UserName: "닉네임-" + alarmPrivacySentinel,
	}
	params := map[string]any{"sub_command": "오타-" + alarmPrivacySentinel}

	if err := command.handleInvalid(t.Context(), cmdCtx, params); err != nil {
		t.Fatalf("handleInvalid() error = %v", err)
	}

	logged := logs.String()
	if !strings.Contains(logged, `"`+privacylog.KeyRoomID+`":"123456789"`) {
		t.Fatalf("log record lost the room correlation key: %s", logged)
	}
	if strings.Contains(logged, alarmPrivacySentinel) {
		t.Fatalf("log record leaked user input: %s", logged)
	}
}

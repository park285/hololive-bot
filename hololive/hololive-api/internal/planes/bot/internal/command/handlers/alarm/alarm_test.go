package alarm

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

func TestCommandIdentityAndDefaultAction(t *testing.T) {
	command := NewAlarmCommand(&handlercore.Dependencies{})
	if command.Name() != "alarm" || command.Description() == "" {
		t.Fatalf("command identity = (%q, %q)", command.Name(), command.Description())
	}
	if got := alarmAction(nil); got != "list" {
		t.Fatalf("alarmAction(nil) = %q, want list", got)
	}
	if got := alarmAction(map[string]any{"action": "clear"}); got != "clear" {
		t.Fatalf("alarmAction(clear) = %q, want clear", got)
	}
}

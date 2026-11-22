package alarm

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

func TestCommandIdentityAndDefaultAction(t *testing.T) {
	command := NewAlarmCommand(&handlercore.Dependencies{})
	if command.Name() != "alarm" || command.Description() == "" {
		t.Fatalf("command identity = (%q, %q)", command.Name(), command.Description())
	}

	if got := alarmAction(botcommand.AlarmParams{}); got != "list" {
		t.Fatalf("alarmAction(botcommand.AlarmParams{}) = %q, want list", got)
	}

	if got := alarmAction(botcommand.AlarmParams{Action: "clear"}); got != "clear" {
		t.Fatalf("alarmAction(clear) = %q, want clear", got)
	}
}

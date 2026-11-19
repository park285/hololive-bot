package botcommand_test

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

func TestCommandType_String(t *testing.T) {
	t.Parallel()

	got := botcommand.CommandLive.String()
	if got != "live" {
		t.Errorf("CommandLive.String() = %q, want %q", got, "live")
	}
}

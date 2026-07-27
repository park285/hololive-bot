package orchestration

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command"
)

func TestCommandInitViewWiresHelpImageRenderer(t *testing.T) {
	deps := (&Bot{}).commandInitView().toCommandDependencies(command.NewRegistry())
	if deps.HelpImageRenderer == nil {
		t.Fatal("help image renderer must be initialized")
	}
}

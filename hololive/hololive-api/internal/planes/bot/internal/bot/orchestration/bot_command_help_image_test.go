package orchestration

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

func TestCommandInitViewWiresHelpImageRenderer(t *testing.T) {
	view := (&Bot{}).commandInitView()
	deps := view.toCommandDependencies(handlers.NewRegistry())
	if deps.HelpImageRenderer == nil {
		t.Fatal("help image renderer must be initialized")
	}
}

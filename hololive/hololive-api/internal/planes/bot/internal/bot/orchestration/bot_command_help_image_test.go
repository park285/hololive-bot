package orchestration

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
)

func TestCommandInitViewWiresHelpImageProvider(t *testing.T) {
	view := (&Bot{}).commandInitView()
	deps := view.toCommandDependencies(handlers.NewRegistry())
	if deps.HelpImageProvider == nil {
		t.Fatal("help image provider must be initialized")
	}
	if deps.SendImages == nil {
		t.Fatal("help image album sender must be initialized")
	}
}

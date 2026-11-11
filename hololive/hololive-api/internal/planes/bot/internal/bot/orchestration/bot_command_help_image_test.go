package orchestration

import "testing"

func TestCommandInitViewWiresHelpImageProvider(t *testing.T) {
	view := (&Bot{}).commandInitView(&Dependencies{}, nil)
	deps := view.toCommandDependencies()

	if deps.HelpImageProvider == nil {
		t.Fatal("help image provider must be initialized")
	}

	if deps.SendImages == nil {
		t.Fatal("help image album sender must be initialized")
	}
}

package orchestration

import (
	"testing"

	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestCommandInitViewWiresHelpImageProvider(t *testing.T) {
	view := (&Bot{postgres: databasemocks.NewUnconnectedClient(t)}).commandInitView(&Dependencies{}, nil, nil)
	deps := view.toCommandDependencies()

	if deps.HelpImageProvider == nil {
		t.Fatal("help image provider must be initialized")
	}

	if deps.SendImages == nil {
		t.Fatal("help image album sender must be initialized")
	}
}

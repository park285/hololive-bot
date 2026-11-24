package messaging

import (
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
)

const (
	testMemberPekora     = "페코라"
	testMemberSakuraMiko = "사쿠라 미코"
)

func typedParams[P botcommand.Params](t *testing.T, cmd *ParsedCommand) P {
	t.Helper()

	params, ok := cmd.Params.(P)
	if !ok {
		t.Fatalf("params = %T, want %T", cmd.Params, params)
	}

	return params
}

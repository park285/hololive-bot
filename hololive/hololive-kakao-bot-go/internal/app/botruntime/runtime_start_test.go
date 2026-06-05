package botruntime

import (
	"context"
	"testing"
)

func TestBotRuntimeStartStartsH3CertReload(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var got context.Context
	r := &BotRuntime{h3CertReloadStart: func(c context.Context) { got = c }}

	r.Start(ctx, nil)

	if got != ctx {
		t.Fatalf("h3CertReloadStart ctx = %v, want start ctx", got)
	}
}

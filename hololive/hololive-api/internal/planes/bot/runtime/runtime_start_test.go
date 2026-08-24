package botruntime

import (
	"context"
	"testing"
)

func TestBotRuntimeStartStartsH3CertReload(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	seen := make(chan context.Context, 1)
	r := &BotRuntime{h3CertReloadStart: func(c context.Context) { seen <- c }}

	r.Start(ctx, nil)

	select {
	case got := <-seen:
		if got != ctx {
			t.Fatalf("h3CertReloadStart ctx = %v, want start ctx", got)
		}
	default:
		t.Fatal("h3CertReloadStart was not invoked")
	}
}

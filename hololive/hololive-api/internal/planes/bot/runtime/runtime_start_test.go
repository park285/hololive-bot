package botruntime

import (
	"context"
	"testing"
	"time"
)

func TestBotRuntimeStartStartsH3CertReload(t *testing.T) {
	t.Parallel()

	seen := make(chan context.Context, 1)
	r := &BotRuntime{h3CertReloadStart: func(ctx context.Context) { seen <- ctx }}

	r.Start(t.Context(), nil)
	t.Cleanup(r.Close)

	select {
	case got := <-seen:
		if got.Err() != nil {
			t.Fatalf("reload context already canceled: %v", got.Err())
		}

		if err := r.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}

		if got.Err() == nil {
			t.Fatal("shutdown did not cancel reload context")
		}
	case <-time.After(time.Second):
		t.Fatal("H3 certificate reload was not started")
	}
}

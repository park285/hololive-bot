package runtime

import (
	"context"
	"testing"
)

func TestStartRunsH3CertReloadHookWithRunContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var got context.Context
	Start(ctx, nil, StartHooks{
		StartH3CertReload: func(c context.Context) { got = c },
	})

	if got != ctx {
		t.Fatalf("StartH3CertReload ctx = %v, want run ctx", got)
	}
}

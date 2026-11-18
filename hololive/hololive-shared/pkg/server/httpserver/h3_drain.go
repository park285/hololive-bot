package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/quic-go/quic-go/http3"
)

type h3RequestDrain struct {
	active atomic.Int64
}

func (d *h3RequestDrain) wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.active.Add(1)
		defer d.active.Add(-1)

		next.ServeHTTP(w, r)
	})
}

func (d *h3RequestDrain) drained() bool {
	return d != nil && d.active.Load() == 0
}

func shutdownH3Server(ctx context.Context, server *http3.Server, drain *h3RequestDrain) error {
	if server == nil {
		return nil
	}

	shutdownCtx, abort := context.WithCancel(context.WithoutCancel(ctx))
	defer abort()

	done := make(chan error, 1)

	go func() {
		done <- server.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP/3 server shutdown failed: %w", err)
		}

		return nil
	case <-ctx.Done():
		idleOnly := drain.drained()

		abort()

		if err := <-done; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP/3 server shutdown failed: %w", err)
		}

		if idleOnly {
			return nil
		}

		return fmt.Errorf("HTTP/3 server shutdown failed: %w", ctx.Err())
	}
}

package adminruntime

import (
	"context"
	"errors"
	"fmt"

	applifecycle "github.com/kapu/hololive-shared/pkg/applifecycle"
)

func (r *AdminAPIRuntime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.stateMu.Lock()

	r.closing = true

	backgroundCancel, backgroundDone := r.backgroundCancel, r.backgroundDone
	r.stateMu.Unlock()

	if backgroundCancel != nil {
		backgroundCancel()
	}

	httpErr := applifecycle.Shutdown(ctx, applifecycle.ShutdownHooks{
		Logger:             r.Logger,
		ShutdownHTTPServer: r.ShutdownHTTPServer,
	})
	backgroundErr := waitAdminBackground(ctx, backgroundDone)

	r.stateMu.Lock()

	if r.drainErr == nil {
		r.drainErr = errors.Join(httpErr, backgroundErr)
	}

	if r.httpStopErr != nil && !errors.Is(r.drainErr, r.httpStopErr) {
		r.drainErr = errors.Join(r.drainErr, r.httpStopErr)
	}

	resultErr := r.drainErr
	r.stateMu.Unlock()

	if resultErr != nil {
		return fmt.Errorf("shutdown: %w", resultErr)
	}

	return nil
}

func (r *AdminAPIRuntime) ShutdownHTTPServer(ctx context.Context) error {
	if r == nil {
		return nil
	}

	if r.HTTPServers == nil {
		r.stateMu.Lock()

		r.httpQuiesced = true
		r.stateMu.Unlock()

		return nil
	}

	r.httpStopInit.Do(func() { r.httpStopGate = make(chan struct{}, 1) })

	select {
	case r.httpStopGate <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("shutdown: wait for admin HTTP stop: %w", ctx.Err())
	}

	r.stateMu.Lock()

	quiesced, previousErr := r.httpQuiesced, r.httpStopErr
	r.stateMu.Unlock()

	if quiesced {
		<-r.httpStopGate

		return previousErr
	}

	done := make(chan error, 1)

	go func() {
		stopErr := r.HTTPServers.Shutdown(ctx)

		r.stateMu.Lock()

		if stopErr != nil && r.httpStopErr == nil {
			r.httpStopErr = stopErr
		}

		r.httpQuiesced = stopErr == nil

		reportErr := r.httpStopErr
		r.stateMu.Unlock()

		done <- reportErr

		<-r.httpStopGate
	}()

	select {
	case stopErr := <-done:
		if stopErr != nil {
			return fmt.Errorf("shutdown: %w", stopErr)
		}

		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown: admin HTTP stop: %w", ctx.Err())
	}
}

func waitAdminBackground(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	default:
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("join admin background tasks: %w", ctx.Err())
	}
}

func (r *AdminAPIRuntime) ResourceUsersJoined() bool {
	return r == nil || r.quiesced()
}

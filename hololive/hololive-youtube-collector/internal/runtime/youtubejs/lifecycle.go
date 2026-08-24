package youtubejs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

func (h *Helper) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}

	h.closeOnce.Do(func() {
		h.closeErr = h.shutdown(ctx)
	})

	return h.closeErr
}

func (h *Helper) shutdown(ctx context.Context) error {
	closeIdle(h)

	if h.cmd == nil || h.cmd.Process == nil {
		h.closeWaited()

		if err := h.removeRuntime(); err != nil {
			return fmt.Errorf("remove runtime: %w", err)
		}

		return nil
	}

	closeCtx, cancel := shutdownContext(ctx, h.shutdownTimeout)

	defer cancel()

	termErr := h.signalTerm()
	waitErr := h.waitExit(closeCtx)

	if h.Exited() {
		return errors.Join(termErr, waitErr, h.removeRuntime())
	}

	killErr := h.signalKill()
	if reapErr := h.reapAfterKill(); reapErr != nil {
		return errors.Join(termErr, waitErr, killErr, reapErr, ErrCleanupTimedOut, h.removeRuntime())
	}

	return errors.Join(termErr, killErr, h.removeRuntime())
}

func (h *Helper) closeWaited() {
	if h.waited == nil {
		return
	}

	select {
	case <-h.waited:
	default:
		close(h.waited)
	}
}

func shutdownContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}

	if ctx == nil || ctx.Err() != nil {
		return context.WithTimeout(context.Background(), timeout)
	}

	return context.WithTimeout(ctx, timeout)
}

func closeIdle(h *Helper) {
	if h.rpc != nil && h.rpc.http != nil {
		h.rpc.http.CloseIdleConnections()
	}

	if h.health != nil {
		h.health.CloseIdleConnections()
	}
}

func (h *Helper) signalTerm() error {
	if h.cmd == nil || h.cmd.Process == nil || h.Exited() {
		return nil
	}

	if err := h.cmd.Process.Signal(syscall.SIGTERM); err != nil && !isFinished(err) {
		return fmt.Errorf("stop youtube.js helper: %w", err)
	}

	return nil
}

func (h *Helper) signalKill() error {
	if h.cmd == nil || h.cmd.Process == nil || h.Exited() {
		return nil
	}

	h.forcedKills.Add(1)

	if err := h.cmd.Process.Kill(); err != nil && !isFinished(err) {
		return fmt.Errorf("stop youtube.js helper: %w", err)
	}

	return nil
}

func (h *Helper) waitExit(ctx context.Context) error {
	if h.waited == nil {
		return nil
	}

	select {
	case <-h.waited:
		return h.waitErr
	case <-ctx.Done():
		return fmt.Errorf("stop youtube.js helper: wait timed out: %w", ctx.Err())
	}
}

func (h *Helper) reapAfterKill() error {
	if h.Exited() {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)

	defer cancel()

	select {
	case <-h.waited:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop youtube.js helper: reap timed out: %w", ctx.Err())
	}
}

func (h *Helper) removeRuntime() error {
	if h.runtimeDir == "" {
		return nil
	}

	if err := os.RemoveAll(h.runtimeDir); err != nil {
		return fmt.Errorf("remove youtube.js helper runtime: %w", err)
	}

	return nil
}

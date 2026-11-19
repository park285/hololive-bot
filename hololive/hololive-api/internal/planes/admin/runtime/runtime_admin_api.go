package adminruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type backgroundTask interface {
	Start(ctx context.Context)
}

type AdminAPIRuntime struct {
	Config *apiconfig.AdminPlaneConfig
	Logger *slog.Logger

	ServerAddr     string
	HTTPServers    *sharedserver.RuntimeHTTPServers
	PhotoSync      backgroundTask
	SchedulePoster backgroundTask

	stateMu          sync.Mutex
	started          bool
	closing          bool
	backgroundCancel context.CancelFunc
	backgroundDone   chan struct{}

	httpStopInit  sync.Once
	httpStopGate  chan struct{}
	httpStartOnce sync.Once
	httpStopErr   error
	httpQuiesced  bool
	drainErr      error
	cleanup       func() error
	cleanupOnce   sync.Once
	cleanupDone   chan struct{}
	cleanupErr    error
}

func (r *AdminAPIRuntime) CloseContext(ctx context.Context) error {
	if r == nil {
		return nil
	}

	r.stateMu.Lock()

	r.closing = true

	started := r.started
	r.stateMu.Unlock()

	var drainErr error

	if started {
		drainErr = r.Shutdown(ctx)
		if !r.quiesced() {
			return fmt.Errorf("close admin runtime: drain: %w", drainErr)
		}
	}

	r.cleanupOnce.Do(func() {
		r.cleanupDone = make(chan struct{})

		go func() {
			defer close(r.cleanupDone)

			if r.cleanup != nil {
				r.cleanupErr = r.cleanup()
			}
		}()
	})

	select {
	case <-r.cleanupDone:
		return errors.Join(drainErr, r.cleanupErr)
	default:
	}

	select {
	case <-r.cleanupDone:
		return errors.Join(drainErr, r.cleanupErr)
	case <-ctx.Done():
		return errors.Join(drainErr, fmt.Errorf("close admin runtime: resource cleanup: %w", ctx.Err()))
	}
}

func (r *AdminAPIRuntime) quiesced() bool {
	r.stateMu.Lock()

	httpQuiesced, backgroundDone := r.httpQuiesced, r.backgroundDone
	closedBeforeStart := r.closing && !r.started
	r.stateMu.Unlock()

	if closedBeforeStart {
		return true
	}

	if !httpQuiesced {
		return false
	}

	if backgroundDone == nil {
		return true
	}

	select {
	case <-backgroundDone:
		return true
	default:
		return false
	}
}

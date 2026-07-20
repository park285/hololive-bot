package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/park285/iris-client-go/iris"
)

const runtimeIrisH3DialGuardTTL = time.Minute

type runtimeIrisH3DialGuard struct {
	resolveBaseURL func() (string, error)
	logger         *slog.Logger
	mu             sync.Mutex
	baseURL        string
	guard          func(context.Context, net.IP) error
}

func newRuntimeIrisH3DialGuard(resolveBaseURL func() (string, error), logger *slog.Logger) *runtimeIrisH3DialGuard {
	return &runtimeIrisH3DialGuard{resolveBaseURL: resolveBaseURL, logger: logger}
}

func (g *runtimeIrisH3DialGuard) allow(ctx context.Context, ip net.IP) error {
	guard, err := g.guardForCurrentBaseURL(ctx)
	if err != nil {
		return err
	}
	return guard(ctx, ip)
}

func (g *runtimeIrisH3DialGuard) guardForCurrentBaseURL(ctx context.Context) (func(context.Context, net.IP) error, error) {
	if g.resolveBaseURL == nil {
		return nil, fmt.Errorf("iris h3 egress guard has no base URL resolver")
	}
	baseURL, err := g.resolveBaseURL()
	if err != nil {
		return nil, fmt.Errorf("resolve Iris base URL for H3 egress guard: %w", err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.guard != nil && g.baseURL == baseURL {
		return g.guard, nil
	}
	guard, err := iris.NewH3DialGuardForBaseURL(
		ctx,
		baseURL,
		iris.WithH3DialGuardTTL(runtimeIrisH3DialGuardTTL),
		iris.WithH3DialGuardLogger(g.logger),
	)
	if err != nil {
		return nil, fmt.Errorf("configure Iris H3 dial guard: %w", err)
	}
	g.baseURL = baseURL
	g.guard = guard
	return guard, nil
}

package api

import (
	"context"
	"fmt"
)

type settingsOperationGate struct {
	requests chan struct{}
}

func newSettingsOperationGate() *settingsOperationGate {
	return &settingsOperationGate{requests: make(chan struct{}, 1)}
}

func (g *settingsOperationGate) acquire(ctx context.Context) (func(), error) {
	select {
	case g.requests <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for settings operation: %w", ctx.Err())
	}

	if err := ctx.Err(); err != nil {
		<-g.requests

		return nil, fmt.Errorf("before settings operation: %w", err)
	}

	return func() { <-g.requests }, nil
}

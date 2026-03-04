// Package telemetry: hololive-shared/pkg/telemetry 호환 래퍼.
package telemetry

import (
	"context"
	"fmt"

	sharedtelemetry "github.com/kapu/hololive-shared/pkg/telemetry"
	"go.opentelemetry.io/otel/propagation"
)

type Config = sharedtelemetry.Config
type Provider = sharedtelemetry.Provider
type MapCarrier = sharedtelemetry.MapCarrier

func DefaultConfig() Config {
	return sharedtelemetry.DefaultConfig()
}

func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	provider, err := sharedtelemetry.NewProvider(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new telemetry provider: %w", err)
	}
	return provider, nil
}

func InjectContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	sharedtelemetry.InjectContext(ctx, carrier)
}

func ExtractContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return sharedtelemetry.ExtractContext(ctx, carrier)
}

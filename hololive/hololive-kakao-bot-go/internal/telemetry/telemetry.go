// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

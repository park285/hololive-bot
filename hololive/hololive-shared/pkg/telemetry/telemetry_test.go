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

package telemetry

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should be disabled")
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("expected sample rate 1.0, got %f", cfg.SampleRate)
	}
	if cfg.OTLPInsecure {
		t.Error("default should be secure")
	}
	if cfg.MetricsEnabled {
		t.Error("default metrics should be disabled")
	}
	if cfg.MetricsExportInterval != 30*time.Second {
		t.Errorf("expected metrics export interval 30s, got %v", cfg.MetricsExportInterval)
	}
}

func TestNewProvider_Disabled(t *testing.T) {
	cfg := Config{Enabled: false, MetricsEnabled: false}
	provider, err := NewProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("provider should not be nil")
	}
	if provider.IsEnabled() {
		t.Error("disabled provider should return false for IsEnabled")
	}
}

func TestProvider_Shutdown_Nil(t *testing.T) {
	provider := &Provider{}
	err := provider.Shutdown(context.Background())
	if err != nil {
		t.Errorf("shutdown on nil provider should not error: %v", err)
	}
}

func TestProvider_IsEnabled(t *testing.T) {
	t.Run("nil providers", func(t *testing.T) {
		p := &Provider{tracerProvider: nil, meterProvider: nil}
		if p.IsEnabled() {
			t.Error("should return false when both providers are nil")
		}
	})

	t.Run("meter provider only", func(t *testing.T) {
		p := &Provider{meterProvider: sdkmetric.NewMeterProvider()}
		if !p.IsEnabled() {
			t.Error("should return true when meter provider is enabled")
		}
		if !p.IsMetricsEnabled() {
			t.Error("should return true for metrics enabled")
		}
		if p.IsTracingEnabled() {
			t.Error("should return false for tracing disabled")
		}
	})
}

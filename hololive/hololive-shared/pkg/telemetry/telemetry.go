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

// Package telemetry: OpenTelemetry 기반 분산 추적/메트릭 exporter 초기화 유틸입니다.
package telemetry

import (
	"context"
	stdErrors "errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config: OpenTelemetry 설정입니다.
type Config struct {
	// Enabled: true면 tracing exporter를 활성화합니다.
	Enabled bool

	// MetricsEnabled: true면 metrics exporter를 활성화합니다.
	// 기존 Prometheus `/metrics`와 병행 가능하도록 분리합니다.
	MetricsEnabled bool

	// MetricsExportInterval: OTel metrics export 주기입니다.
	MetricsExportInterval time.Duration

	// ServiceName: 서비스 식별자입니다.
	ServiceName string

	// ServiceVersion: 서비스 버전입니다.
	ServiceVersion string

	// Environment: 배포 환경입니다.
	Environment string

	// OTLPEndpoint: OTLP collector/exporter 주소입니다.
	OTLPEndpoint string

	// OTLPInsecure: true면 TLS 없이 연결합니다. 내부망 전용.
	OTLPInsecure bool

	// SampleRate: tracing 샘플링 비율(0.0~1.0).
	SampleRate float64
}

// DefaultConfig: 기본 설정을 반환합니다(기본은 비활성화).
func DefaultConfig() Config {
	return Config{
		Enabled:               false,
		MetricsEnabled:        false,
		MetricsExportInterval: 30 * time.Second,
		SampleRate:            1.0,
		OTLPInsecure:          false,
	}
}

// Provider: OpenTelemetry provider를 관리합니다.
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
}

// NewProvider: Tracer/Meter Provider를 초기화하고 글로벌로 설정합니다.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled && !cfg.MetricsEnabled {
		return &Provider{}, nil
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	)

	provider := &Provider{}

	if cfg.Enabled {
		exporterOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			exporterOpts = append(exporterOpts,
				otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
		}

		traceExporter, err := otlptracegrpc.New(ctx, exporterOpts...)
		if err != nil {
			return nil, fmt.Errorf("create trace exporter: %w", err)
		}

		var rootSampler sdktrace.Sampler
		if cfg.SampleRate >= 1.0 {
			rootSampler = sdktrace.AlwaysSample()
		} else if cfg.SampleRate <= 0 {
			rootSampler = sdktrace.NeverSample()
		} else {
			rootSampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
		}
		sampler := sdktrace.ParentBased(rootSampler)

		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
		)

		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(
			propagation.NewCompositeTextMapPropagator(
				propagation.TraceContext{},
				propagation.Baggage{},
			),
		)
		provider.tracerProvider = tp
	}

	if cfg.MetricsEnabled {
		metricInterval := cfg.MetricsExportInterval
		if metricInterval <= 0 {
			metricInterval = 30 * time.Second
		}

		metricExporterOpts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
		}
		if cfg.OTLPInsecure {
			metricExporterOpts = append(metricExporterOpts,
				otlpmetricgrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
		}

		metricExporter, err := otlpmetricgrpc.New(ctx, metricExporterOpts...)
		if err != nil {
			return nil, fmt.Errorf("create metric exporter: %w", err)
		}

		reader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(metricInterval))
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(reader),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(mp)
		provider.meterProvider = mp
	}

	return provider, nil
}

// Shutdown: 초기화된 provider들을 종료합니다.
func (p *Provider) Shutdown(ctx context.Context) error {
	var shutdownErrs []error

	if p.tracerProvider != nil {
		if err := p.tracerProvider.Shutdown(ctx); err != nil {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("shutdown tracer provider: %w", err))
		}
	}
	if p.meterProvider != nil {
		if err := p.meterProvider.Shutdown(ctx); err != nil {
			shutdownErrs = append(shutdownErrs, fmt.Errorf("shutdown meter provider: %w", err))
		}
	}

	if len(shutdownErrs) > 0 {
		return fmt.Errorf("shutdown telemetry providers: %w", stdErrors.Join(shutdownErrs...))
	}
	return nil
}

// IsEnabled: tracing/metrics 중 하나라도 활성화되었는지 반환.
func (p *Provider) IsEnabled() bool {
	return p.IsTracingEnabled() || p.IsMetricsEnabled()
}

// IsTracingEnabled: tracing provider 활성화 여부.
func (p *Provider) IsTracingEnabled() bool {
	return p.tracerProvider != nil
}

// IsMetricsEnabled: metrics provider 활성화 여부.
func (p *Provider) IsMetricsEnabled() bool {
	return p.meterProvider != nil
}

// InjectContext: context에서 trace context를 carrier에 주입합니다.
func InjectContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractContext: carrier에서 trace context를 추출해 새 context를 반환합니다.
func ExtractContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// MapCarrier: map[string]string 기반 TextMapCarrier 어댑터.
type MapCarrier map[string]string

// Get: key 값 반환.
func (c MapCarrier) Get(key string) string {
	return c[key]
}

// Set: key 값 설정.
func (c MapCarrier) Set(key, value string) {
	c[key] = value
}

// Keys: 전체 key 반환.
func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

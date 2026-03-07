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

package config

import "time"

// TelemetryConfig: OpenTelemetry 분산 추적 설정
type TelemetryConfig struct {
	Enabled               bool          // 트레이싱 활성화 여부
	MetricsEnabled        bool          // OTel metrics export 활성화 여부 (Prometheus와 병행 가능)
	MetricsExportInterval time.Duration // OTel metrics export 주기
	ServiceName           string        // 서비스 식별자 (ex "hololive-bot")
	ServiceVersion        string        // 서비스 버전 (ex "1.0.0")
	Environment           string        // 배포 환경 (ex "production")
	OTLPEndpoint          string        // OTLP collector 주소 (ex "otel-collector:4317")
	OTLPInsecure          bool          // TLS 없이 연결 (내부망 전용)
	SampleRate            float64       // 샘플링 비율 (0.0 ~ 1.0)
}

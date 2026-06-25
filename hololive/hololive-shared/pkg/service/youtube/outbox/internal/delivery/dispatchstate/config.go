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

package dispatchstate

import "time"

const defaultTelemetryRetention = 24 * time.Hour

type Config struct {
	BatchSize                   int           // 한 번에 처리할 알림 수
	LockTimeout                 time.Duration // 락 타임아웃 (처리 중 상태 유지 시간)
	PollInterval                time.Duration // 폴링 간격
	MaxRetries                  int           // 최대 재시도 횟수
	RetryBackoff                time.Duration // 재시도 간격
	CleanupAfter                time.Duration // 완료된 알림 정리 기간
	CleanupEnabled              bool          // 정리 활성화 여부
	ReviveEnabled               bool          // stale-failed revival sweep 활성화 여부
	ReviveInterval              time.Duration // revival sweep 주기
	ReviveFreshnessWindow       time.Duration // 되살릴 FAILED 알람의 최대 콘텐츠 신선도(created_at 기준)
	ClaimFreshnessWindow        time.Duration
	DeliveryParallelism         int           // room/delivery send 제한 병렬성
	DeliverySendTimeout         time.Duration // room 단위 메시지 발송 1회 최대 시간
	SubscriberLookupParallelism int           // 채널별 구독자 조회 제한 병렬성
	AggregateSyncInterval       time.Duration // aggregate sync maintenance interval
	TelemetryPollInterval       time.Duration // telemetry loop polling interval
	TelemetryBackfillBatch      int           // delivery 상태에서 telemetry 버퍼로 역보강할 최대 건수
	TelemetryFlushBatch         int           // telemetry 버퍼 플러시 최대 건수
	TelemetryRetryBackoff       time.Duration // telemetry 플러시 실패 재시도 간격
	TelemetryRetention          time.Duration // telemetry 버퍼 최소 보존 기간
}

func DefaultConfig() Config {
	return Config{
		BatchSize:             50,
		LockTimeout:           5 * time.Minute,
		PollInterval:          2 * time.Second,
		MaxRetries:            3,
		RetryBackoff:          1 * time.Minute,
		CleanupAfter:          7 * 24 * time.Hour, // 7일
		CleanupEnabled:        true,
		ReviveEnabled:         true,
		ReviveInterval:        5 * time.Minute,
		ReviveFreshnessWindow: 60 * time.Minute,
		// revive window보다 커야 되살린 PENDING(created_at 최대 60m)이 primary claim에서 탈락하지 않는다.
		ClaimFreshnessWindow:        2 * time.Hour,
		DeliveryParallelism:         4,
		DeliverySendTimeout:         10 * time.Second,
		SubscriberLookupParallelism: 16,
		AggregateSyncInterval:       30 * time.Second,
		TelemetryPollInterval:       30 * time.Second,
		TelemetryBackfillBatch:      200,
		TelemetryFlushBatch:         200,
		TelemetryRetryBackoff:       30 * time.Second,
		TelemetryRetention:          defaultTelemetryRetention,
	}
}

func NormalizeDispatcherConfig(config *Config) Config {
	if config == nil {
		defaults := DefaultConfig()
		config = &defaults
	}
	defaults := DefaultConfig()
	normalizeDispatcherCoreConfig(config, &defaults)
	normalizeDispatcherDeliveryConfig(config, &defaults)
	normalizeDispatcherTelemetryConfig(config, &defaults)
	return *config
}

func normalizeDispatcherCoreConfig(config, defaults *Config) {
	if config.BatchSize <= 0 {
		config.BatchSize = defaults.BatchSize
	}
	if config.LockTimeout <= 0 {
		config.LockTimeout = defaults.LockTimeout
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaults.PollInterval
	}
	if config.AggregateSyncInterval <= 0 {
		config.AggregateSyncInterval = defaults.AggregateSyncInterval
	}
	if config.ReviveInterval <= 0 {
		config.ReviveInterval = defaults.ReviveInterval
	}
	if config.ReviveFreshnessWindow <= 0 {
		config.ReviveFreshnessWindow = defaults.ReviveFreshnessWindow
	}
	if config.ClaimFreshnessWindow <= 0 {
		config.ClaimFreshnessWindow = defaults.ClaimFreshnessWindow
	}
	minClaimFreshnessWindow := config.ReviveFreshnessWindow + config.ReviveInterval
	if config.ClaimFreshnessWindow < minClaimFreshnessWindow {
		config.ClaimFreshnessWindow = minClaimFreshnessWindow
	}
}

func normalizeDispatcherDeliveryConfig(config, defaults *Config) {
	if config.DeliveryParallelism <= 0 {
		config.DeliveryParallelism = defaults.DeliveryParallelism
	}
	if config.DeliverySendTimeout <= 0 {
		config.DeliverySendTimeout = defaults.DeliverySendTimeout
	}
	if config.SubscriberLookupParallelism <= 0 {
		config.SubscriberLookupParallelism = defaults.SubscriberLookupParallelism
	}
}

func normalizeDispatcherTelemetryConfig(config, defaults *Config) {
	if config.TelemetryBackfillBatch <= 0 {
		config.TelemetryBackfillBatch = defaults.TelemetryBackfillBatch
	}
	if config.TelemetryPollInterval <= 0 {
		config.TelemetryPollInterval = defaults.TelemetryPollInterval
	}
	if config.TelemetryFlushBatch <= 0 {
		config.TelemetryFlushBatch = defaults.TelemetryFlushBatch
	}
	if config.TelemetryRetryBackoff <= 0 {
		config.TelemetryRetryBackoff = defaults.TelemetryRetryBackoff
	}
	if config.TelemetryRetention <= 0 {
		config.TelemetryRetention = defaults.TelemetryRetention
	}
}

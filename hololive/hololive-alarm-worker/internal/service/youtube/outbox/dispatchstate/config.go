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

import (
	"fmt"
	"strings"
	"time"
)

// Config의 값은 alarm-worker profile의 youtube_delivery 항목이 정본이다. 생성자는 profile 로더가 양수와 신선도 관계를
// 검증한다는 전제로 기본값으로 바꾸거나 값을 끌어올리지 않고, 잘못된 값이면 Validate가 생성 오류로 드러낸다.
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
	AggregateSyncInterval       time.Duration // aggregate 동기화 유지보수 주기
	TelemetryPollInterval       time.Duration // telemetry loop 폴링 주기
	TelemetryFlushBatch         int           // telemetry 버퍼 플러시 최대 건수
	TelemetryRetryBackoff       time.Duration // telemetry 플러시 실패 재시도 간격
	TelemetryRetention          time.Duration // telemetry 버퍼 최소 보존 기간
}

// Validate는 dispatcher가 쓰는 주기·한도·병렬성이 양수인지와, claim 신선도 기간이 revive 기간과 주기의 합 이상인지
// 확인한다. 그보다 claim 기간이 짧으면 되살린 PENDING이 primary claim에서 탈락한다.
func (c *Config) Validate() error {
	required := []struct {
		name  string
		valid bool
	}{
		{"batch size", c.BatchSize > 0},
		{"lock timeout", c.LockTimeout > 0},
		{"poll interval", c.PollInterval > 0},
		{"max retries", c.MaxRetries > 0},
		{"retry backoff", c.RetryBackoff > 0},
		{"revive interval", c.ReviveInterval > 0},
		{"revive freshness window", c.ReviveFreshnessWindow > 0},
		{"claim freshness window", c.ClaimFreshnessWindow > 0},
		{"delivery parallelism", c.DeliveryParallelism > 0},
		{"delivery send timeout", c.DeliverySendTimeout > 0},
		{"subscriber lookup parallelism", c.SubscriberLookupParallelism > 0},
		{"aggregate sync interval", c.AggregateSyncInterval > 0},
		{"telemetry poll interval", c.TelemetryPollInterval > 0},
		{"telemetry flush batch", c.TelemetryFlushBatch > 0},
		{"telemetry retry backoff", c.TelemetryRetryBackoff > 0},
		{"telemetry retention", c.TelemetryRetention > 0},
	}

	var invalid []string

	for _, setting := range required {
		if !setting.valid {
			invalid = append(invalid, setting.name)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("settings must be positive: %s", strings.Join(invalid, ", "))
	}

	if minimum := c.ReviveFreshnessWindow + c.ReviveInterval; c.ClaimFreshnessWindow < minimum {
		return fmt.Errorf("claim freshness window %s must be at least revive freshness window plus revive interval (%s)", c.ClaimFreshnessWindow, minimum)
	}

	return nil
}

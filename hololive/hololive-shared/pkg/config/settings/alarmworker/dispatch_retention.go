// Package alarmworker: alarm-worker 런타임 전용 설정을 소유한다.
// proactive notification egress와 alarm scheduler 역할은 이 런타임만 가진다.
package alarmworker

import (
	"fmt"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

type DispatchRetentionConfig struct {
	Enabled         bool
	Interval        time.Duration
	QueryTimeout    time.Duration
	Limit           int
	SentDays        int
	DLQDays         int
	QuarantinedDays int
	CancelledDays   int
	EventDays       int
}

func loadDispatchRetentionConfig() (DispatchRetentionConfig, error) {
	config := DispatchRetentionConfig{
		Enabled: sharedenv.Bool("ALARM_DISPATCH_RETENTION_ENABLED", true),
	}

	var err error

	if config.Interval, err = load.RequiredMillisDurationEnv("ALARM_DISPATCH_RETENTION_INTERVAL_MS", time.Hour); err != nil {
		return DispatchRetentionConfig{}, fmt.Errorf("required millis duration env: %w", err)
	}

	if config.QueryTimeout, err = load.RequiredMillisDurationEnv("ALARM_DISPATCH_RETENTION_QUERY_TIMEOUT_MS", 30*time.Second); err != nil {
		return DispatchRetentionConfig{}, fmt.Errorf("required millis duration env: %w", err)
	}

	if config.Limit, err = dispatchRetentionLimit(); err != nil {
		return DispatchRetentionConfig{}, fmt.Errorf("alarm dispatch retention limit: %w", err)
	}

	for _, field := range []struct {
		key      string
		fallback int
		target   *int
	}{
		{key: "ALARM_DISPATCH_RETENTION_SENT_DAYS", fallback: 90, target: &config.SentDays},
		{key: "ALARM_DISPATCH_RETENTION_DLQ_DAYS", fallback: 180, target: &config.DLQDays},
		{key: "ALARM_DISPATCH_RETENTION_QUARANTINED_DAYS", fallback: 180, target: &config.QuarantinedDays},
		{key: "ALARM_DISPATCH_RETENTION_CANCELLED_DAYS", fallback: 90, target: &config.CancelledDays}, //nolint:misspell // ALARM_DISPATCH_RETENTION_CANCELLED_DAYS는 배포 환경에 실재하는 환경변수 이름이라 US 철자로 바꾸면 설정이 끊긴다.
		{key: "ALARM_DISPATCH_RETENTION_EVENT_DAYS", fallback: 90, target: &config.EventDays},
	} {
		if *field.target, err = load.RequiredPositiveIntEnv(field.key, field.fallback); err != nil {
			return DispatchRetentionConfig{}, fmt.Errorf("required positive int env: %w", err)
		}
	}

	return config, nil
}

func dispatchRetentionLimit() (int, error) {
	limit, err := load.RequiredPositiveIntEnv("ALARM_DISPATCH_RETENTION_LIMIT", 1000)
	if err != nil {
		return 0, fmt.Errorf("required positive int env: %w", err)
	}

	return min(limit, 10000), nil
}

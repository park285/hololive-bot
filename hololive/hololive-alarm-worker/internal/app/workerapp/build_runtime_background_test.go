package workerapp

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
)

// runner 스위치의 잘못된 값은 "꺼짐"으로 읽히지 않고 기동 오류로 드러나야 한다(stack audit B4).
func TestBackgroundRunnerSwitchRejectsInvalidValue(t *testing.T) {
	for _, tc := range []struct {
		key   string
		build func() optionalRuntimeSchedulerResult
	}{
		{
			key: "CELEBRATION_RUNNER_ENABLED",
			build: func() optionalRuntimeSchedulerResult {
				return buildCelebrationRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
		{
			key: "BIRTHDAY_STREAM_RUNNER_ENABLED",
			build: func() optionalRuntimeSchedulerResult {
				return buildBirthdayStreamRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
	} {
		t.Run(tc.key+"=maybe", func(t *testing.T) {
			t.Setenv(tc.key, "maybe")

			result := tc.build()
			if result.err == nil || !strings.Contains(result.err.Error(), tc.key) {
				t.Fatalf("build error = %v, want invalid %s rejection", result.err, tc.key)
			}

			if result.scheduler != nil {
				t.Fatalf("scheduler = %T, want nil on invalid switch", result.scheduler)
			}
		})

		t.Run(tc.key+"=false", func(t *testing.T) {
			t.Setenv(tc.key, "false")

			result := tc.build()
			if result.err != nil || result.scheduler != nil {
				t.Fatalf("build = (%T, %v), want disabled runner without error", result.scheduler, result.err)
			}
		})
	}
}

// runner 설정 env의 잘못된 값은 기본값으로 바뀌지 않고 기동 오류로 드러나야 한다(holo-alarm-worker-envconfig-silent-defaults).
func TestBackgroundRunnerConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		switchKey string
		key       string
		value     string
		build     func() optionalRuntimeSchedulerResult
	}{
		{
			switchKey: "CELEBRATION_RUNNER_ENABLED",
			key:       "CELEBRATION_CHECK_HOUR_KST",
			value:     "noon",
			build: func() optionalRuntimeSchedulerResult {
				return buildCelebrationRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
		{
			switchKey: "CELEBRATION_RUNNER_ENABLED",
			key:       "CELEBRATION_CHECK_HOUR_KST",
			value:     "24",
			build: func() optionalRuntimeSchedulerResult {
				return buildCelebrationRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
		{
			switchKey: "CELEBRATION_RUNNER_ENABLED",
			key:       "CELEBRATION_RUN_INTERVAL_MS",
			value:     "0",
			build: func() optionalRuntimeSchedulerResult {
				return buildCelebrationRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
		{
			switchKey: "BIRTHDAY_STREAM_RUNNER_ENABLED",
			key:       "BIRTHDAY_STREAM_POLL_INTERVAL_MS",
			value:     "30m",
			build: func() optionalRuntimeSchedulerResult {
				return buildBirthdayStreamRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
		{
			switchKey: "BIRTHDAY_STREAM_RUNNER_ENABLED",
			key:       "BIRTHDAY_STREAM_SESSION_FRESHNESS_MS",
			value:     "-1",
			build: func() optionalRuntimeSchedulerResult {
				return buildBirthdayStreamRunnerScheduler(nil, nil, queue.PublishConfig{}, nil)
			},
		},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.switchKey, "true")
			t.Setenv(tc.key, tc.value)

			result := tc.build()
			if result.err == nil || !strings.Contains(result.err.Error(), tc.key) {
				t.Fatalf("build error = %v, want invalid %s rejection", result.err, tc.key)
			}

			if result.scheduler != nil {
				t.Fatalf("scheduler = %T, want nil on invalid config", result.scheduler)
			}
		})
	}
}

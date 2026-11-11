package config

import (
	"errors"
	"fmt"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

func LoadWorkerProfile() (*AlarmWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv(WorkerIdentity())
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &AlarmWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettingsSet(loaded, map[string]any{
		WorkerAlarmDispatch:   &profile.AlarmDispatch,
		WorkerYouTubeDelivery: &profile.YouTubeDelivery,
	}); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate alarm worker profile: %w", err)
	}

	return profile, nil
}

func validateWorkerProfile(profile *AlarmWorkerProfile) error {
	if profile == nil {
		return errors.New("alarm worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		WorkerAlarmDispatch:   {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeNone},
		WorkerYouTubeDelivery: {AttemptTimeout: workercontract.DurationModeFixed, Capacity: workercontract.CapacityModeUnbounded, MaxAge: workercontract.DurationModeFixed},
	})

	problems = append(problems, positiveValueProblems(profile)...)
	problems = append(problems, positiveIntProblems(profile)...)
	problems = append(problems, relationshipProblems(profile, workers)...)

	if err := runtimepolicy.JoinWorkerProfileProblems("alarm-worker", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func positiveValueProblems(profile *AlarmWorkerProfile) []string {
	return runtimepolicy.PositiveValueProblems(map[string]int64{
		"alarm_dispatch.lease_ms":                     profile.AlarmDispatch.LeaseMS,
		"alarm_dispatch.quarantine_threshold_ms":      profile.AlarmDispatch.QuarantineThresholdMS,
		"alarm_dispatch.handoff_horizon_ms":           profile.AlarmDispatch.HandoffHorizonMS,
		"alarm_dispatch.recovery_interval_ms":         profile.AlarmDispatch.RecoveryIntervalMS,
		"alarm_dispatch.poll_interval_ms":             profile.AlarmDispatch.PollIntervalMS,
		"alarm_dispatch.idle_backoff_min_ms":          profile.AlarmDispatch.IdleBackoffMinMS,
		"alarm_dispatch.idle_backoff_max_ms":          profile.AlarmDispatch.IdleBackoffMaxMS,
		"youtube_delivery.lock_timeout_ms":            profile.YouTubeDelivery.LockTimeoutMS,
		"youtube_delivery.poll_interval_ms":           profile.YouTubeDelivery.PollIntervalMS,
		"youtube_delivery.retry_backoff_ms":           profile.YouTubeDelivery.RetryBackoffMS,
		"youtube_delivery.cleanup_after_ms":           profile.YouTubeDelivery.CleanupAfterMS,
		"youtube_delivery.revive_interval_ms":         profile.YouTubeDelivery.ReviveIntervalMS,
		"youtube_delivery.revive_freshness_window_ms": profile.YouTubeDelivery.ReviveFreshnessWindowMS,
		"youtube_delivery.aggregate_sync_interval_ms": profile.YouTubeDelivery.AggregateSyncIntervalMS,
		"youtube_delivery.telemetry_poll_interval_ms": profile.YouTubeDelivery.TelemetryPollIntervalMS,
		"youtube_delivery.telemetry_retry_backoff_ms": profile.YouTubeDelivery.TelemetryRetryBackoffMS,
		"youtube_delivery.telemetry_retention_ms":     profile.YouTubeDelivery.TelemetryRetentionMS,
		"youtube_delivery.handoff_horizon_ms":         profile.YouTubeDelivery.HandoffHorizonMS,
	})
}

func positiveIntProblems(profile *AlarmWorkerProfile) []string {
	return runtimepolicy.PositiveIntProblems(map[string]int{
		"alarm_dispatch.recovery_batch_size":             profile.AlarmDispatch.RecoveryBatchSize,
		"alarm_dispatch.max_batch":                       profile.AlarmDispatch.MaxBatch,
		"alarm_dispatch.max_batches_per_wake":            profile.AlarmDispatch.MaxBatchesPerWake,
		"youtube_delivery.batch_size":                    profile.YouTubeDelivery.BatchSize,
		"youtube_delivery.max_retries":                   profile.YouTubeDelivery.MaxRetries,
		"youtube_delivery.subscriber_lookup_parallelism": profile.YouTubeDelivery.SubscriberLookupParallelism,
		"youtube_delivery.telemetry_flush_batch":         profile.YouTubeDelivery.TelemetryFlushBatch,
	})
}

func relationshipProblems(profile *AlarmWorkerProfile, workers map[string]workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.AlarmDispatch.IdleBackoffMaxMS < profile.AlarmDispatch.IdleBackoffMinMS {
		problems = append(problems, "alarm_dispatch idle backoff range is invalid")
	}

	freshness := workers[WorkerYouTubeDelivery].Queue.MaxAge.Milliseconds
	if freshness != nil && (*freshness < profile.YouTubeDelivery.ReviveFreshnessWindowMS ||
		*freshness-profile.YouTubeDelivery.ReviveFreshnessWindowMS < profile.YouTubeDelivery.ReviveIntervalMS) {
		problems = append(problems, "youtube_delivery max_age must cover revive freshness plus interval")
	}

	alarm := workers[WorkerAlarmDispatch]
	if alarm.Executor.Enabled && alarm.Executor.ConfiguredWorkers != 1 {
		problems = append(problems, "alarm_dispatch currently requires exactly one scheduler worker")
	}

	if timeout := alarm.Executor.AttemptTimeout.Milliseconds; timeout != nil && *timeout >= profile.AlarmDispatch.LeaseMS {
		problems = append(problems, "alarm_dispatch attempt_timeout must be less than lease_ms")
	}

	// Iris는 발송을 최소 간격으로 차례대로 처리하므로 일괄 발송의 handoff 완료는 발송 시도 시간보다 늦을 수 있다.
	// 두 발송 경로가 같은 Iris 큐를 쓰므로 확인 기한은 모두 발송 시도 시간 이상이어야 한다.
	for _, horizon := range []struct {
		worker    string
		horizonMS int64
	}{
		{WorkerAlarmDispatch, profile.AlarmDispatch.HandoffHorizonMS},
		{WorkerYouTubeDelivery, profile.YouTubeDelivery.HandoffHorizonMS},
	} {
		if timeout := workers[horizon.worker].Executor.AttemptTimeout.Milliseconds; timeout != nil && horizon.horizonMS < *timeout {
			problems = append(problems, horizon.worker+" handoff_horizon_ms must be at least attempt_timeout")
		}
	}

	// stale sending 격리 기준이 lease보다 짧으면 lease가 살아 있는 진행 중 발송을 격리한다. dispatch consumer도 같은 관계를
	// 생성 시 강제하지만, 프로필 검증이 기동 전에 키 이름으로 먼저 드러낸다.
	if profile.AlarmDispatch.QuarantineThresholdMS < profile.AlarmDispatch.LeaseMS {
		problems = append(problems, "alarm_dispatch quarantine_threshold_ms must be at least lease_ms")
	}

	if alarmDispatchRecoveryExceedsReplayHorizon(profile.AlarmDispatch) {
		problems = append(problems, "alarm_dispatch lease_ms plus recovery_interval_ms must be less than the Iris automatic replay horizon")
	}

	return problems
}

// 결과 불명(transport ambiguity) 발송 뒤 alarm_dispatch는 send unit에 고정한 client request ID로 다시 보내고, 중복 송출 방지를 Iris reply
// admission dedup에 맡긴다. 그 행을 claim한 worker가 발송 전에 멈추면 행은 lease_ms 만료 뒤 다음 recovery tick(recovery_interval_ms)에서야 재발송
// 대상이 된다. 이 합은 재발송까지의 최소 회수 지연일 뿐이며, 실제 창에는 첫 admission 뒤의 retry backoff·attempt timeout·DrainBatch 주기가 더해진다. 따라서 이 검사는
// 필요조건이다. 기준은 Iris admission 보존 기간에서 그 차이를 흡수할 여유를 뺀 iris.ReplyAutomaticReplayHorizon(iris-client-go 공개 상수)이며, 지평과 같은
// 값도 여유를 잠식하므로 거부한다. YouTube 발송(youtube_delivery)의 lease는 이 지평과 묶이지 않으며,
// lock_timeout_ms가 만료한 SENDING 행은 격리된다.
func alarmDispatchRecoveryExceedsReplayHorizon(settings AlarmDispatchWorkerSettings) bool {
	horizon := iris.ReplyAutomaticReplayHorizon.Milliseconds()
	lease, recovery := settings.LeaseMS, settings.RecoveryIntervalMS

	// 양수가 아닌 값은 positiveValueProblems가 보고한다. 양수 lease에서는 뺄셈이 넘치지 않는다.
	return lease > 0 && recovery > 0 && recovery >= horizon-lease
}

package config

import (
	"strings"
	"testing"

	"github.com/park285/iris-client-go/v5/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestLoadWorkerProfileLoadsExactRoleSettings(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	if _, ok := profile.Loaded.Profile.Workers["alarm_dispatch"]; !ok {
		t.Fatal("alarm worker profile is missing the alarm_dispatch worker")
	}
}

func TestLoadWorkerProfileRejectsWrongRole(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/foreign-role-worker-profile.json")

	if _, err := LoadWorkerProfile(); err == nil || !strings.Contains(err.Error(), "got hololive/api, want hololive/alarm-worker") {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}
}

func TestValidateWorkerProfileRequiresYouTubeLockTimeout(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatalf("LoadWorkerProfile() error = %v", err)
	}

	profile.YouTubeDelivery.LockTimeoutMS = 0

	if err := validateWorkerProfile(profile); err == nil || !strings.Contains(err.Error(), "youtube_delivery.lock_timeout_ms") {
		t.Fatalf("validateWorkerProfile() error = %v, want youtube_delivery lock timeout still required", err)
	}
}

func TestValidateWorkerProfileAlarmAttemptLeaseBoundary(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	worker := profile.Loaded.Profile.Workers[WorkerAlarmDispatch]

	worker.Executor.AttemptTimeout.Milliseconds = new(profile.AlarmDispatch.LeaseMS)
	profile.Loaded.Profile.Workers[WorkerAlarmDispatch] = worker

	if err := validateWorkerProfile(profile); err == nil {
		t.Fatal("attempt equal to lease must be rejected")
	}

	worker.Executor.AttemptTimeout.Milliseconds = new(profile.AlarmDispatch.LeaseMS - 1)
	profile.Loaded.Profile.Workers[WorkerAlarmDispatch] = worker

	if err := validateWorkerProfile(profile); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWorkerProfileAlarmRecoveryDelayWithinReplayHorizon(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	horizon := iris.ReplyAutomaticReplayHorizon.Milliseconds()
	lease := profile.AlarmDispatch.LeaseMS
	recovery := profile.AlarmDispatch.RecoveryIntervalMS

	for _, tc := range []struct {
		name       string
		leaseMS    int64
		recoveryMS int64
		valid      bool
	}{
		{name: "lease fills horizon", leaseMS: horizon - recovery, recoveryMS: recovery},
		{name: "lease one millisecond inside horizon", leaseMS: horizon - recovery - 1, recoveryMS: recovery, valid: true},
		{name: "recovery fills horizon", leaseMS: lease, recoveryMS: horizon - lease},
		{name: "recovery one millisecond inside horizon", leaseMS: lease, recoveryMS: horizon - lease - 1, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile.AlarmDispatch.LeaseMS = tc.leaseMS
			profile.AlarmDispatch.QuarantineThresholdMS = tc.leaseMS
			profile.AlarmDispatch.RecoveryIntervalMS = tc.recoveryMS

			err := validateWorkerProfile(profile)
			if tc.valid && err != nil {
				t.Fatalf("validateWorkerProfile() error = %v, want recovery delay inside replay horizon accepted", err)
			}

			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "alarm_dispatch lease_ms plus recovery_interval_ms")) {
				t.Fatalf("validateWorkerProfile() error = %v, want recovery delay reaching replay horizon rejected", err)
			}
		})
	}
}

func TestValidateWorkerProfileAlarmQuarantineThresholdCoversLease(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	lease := profile.AlarmDispatch.LeaseMS
	threshold := profile.AlarmDispatch.QuarantineThresholdMS

	for _, tc := range []struct {
		name        string
		leaseMS     int64
		thresholdMS int64
		valid       bool
	}{
		{name: "threshold equals lease", leaseMS: lease, thresholdMS: lease, valid: true},
		{name: "threshold one millisecond below lease", leaseMS: lease, thresholdMS: lease - 1},
		{name: "lease one millisecond above threshold", leaseMS: threshold + 1, thresholdMS: threshold},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile.AlarmDispatch.LeaseMS = tc.leaseMS
			profile.AlarmDispatch.QuarantineThresholdMS = tc.thresholdMS

			err := validateWorkerProfile(profile)
			if tc.valid && err != nil {
				t.Fatalf("validateWorkerProfile() error = %v, want threshold covering lease accepted", err)
			}

			if !tc.valid && err == nil {
				t.Fatalf("validateWorkerProfile() error = %v, want threshold below lease rejected", err)
			}
		})
	}
}

func TestWorkerProfileDeclaresDomainAgePolicies(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	if got := profile.Loaded.Profile.Workers[WorkerAlarmDispatch].Queue.MaxAge; got.Mode != workercontract.DurationModeNone || got.Milliseconds != nil {
		t.Fatalf("alarm_dispatch max_age = %+v, want none", got)
	}

	worker := profile.Loaded.Profile.Workers[WorkerYouTubeDelivery]
	if got := worker.Queue.MaxAge.Milliseconds; got == nil || *got != 7200000 {
		t.Fatalf("youtube max_age = %v, want existing two-hour parent freshness", got)
	}

	boundary := profile.YouTubeDelivery.ReviveFreshnessWindowMS + profile.YouTubeDelivery.ReviveIntervalMS

	worker.Queue.MaxAge.Milliseconds = new(boundary)
	profile.Loaded.Profile.Workers[WorkerYouTubeDelivery] = worker

	if err := validateWorkerProfile(profile); err != nil {
		t.Fatal(err)
	}

	worker.Queue.MaxAge.Milliseconds = new(boundary - 1)
	profile.Loaded.Profile.Workers[WorkerYouTubeDelivery] = worker

	if err := validateWorkerProfile(profile); err == nil {
		t.Fatal("max_age shorter than revive freshness plus interval must fail")
	}
}

// Iris는 발송을 최소 2.5초 간격으로 처리하므로 일괄 발송의 handoff 완료가 발송 attempt_timeout보다 늦을 수 있다.
func TestValidateWorkerProfileAlarmHandoffHorizonCoversAttemptTimeout(t *testing.T) {
	settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

	profile, err := LoadWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	attempt := *profile.Loaded.Profile.Workers[WorkerAlarmDispatch].Executor.AttemptTimeout.Milliseconds

	for _, tc := range []struct {
		name      string
		horizonMS int64
		valid     bool
	}{
		{name: "missing horizon", horizonMS: 0},
		{name: "horizon one millisecond below attempt timeout", horizonMS: attempt - 1},
		{name: "horizon equals attempt timeout", horizonMS: attempt, valid: true},
		{name: "horizon covers queued burst", horizonMS: 300_000, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile.AlarmDispatch.HandoffHorizonMS = tc.horizonMS

			err := validateWorkerProfile(profile)
			if tc.valid && err != nil {
				t.Fatalf("validateWorkerProfile() error = %v, want horizon accepted", err)
			}

			if !tc.valid && (err == nil || !strings.Contains(err.Error(), "handoff_horizon_ms")) {
				t.Fatalf("validateWorkerProfile() error = %v, want handoff_horizon_ms rejected", err)
			}
		})
	}
}

// youtube_delivery도 같은 Iris 발송 큐를 거치므로 handoff 확인 기한을 발송 시도 시간과 따로 둔다.
func TestValidateWorkerProfileDeliveryHandoffHorizonCoversAttemptTimeout(t *testing.T) {
	for _, worker := range []struct {
		name    string
		key     string
		id      string
		horizon func(*AlarmWorkerProfile) *int64
	}{
		{
			name: "youtube_delivery", key: "youtube_delivery handoff_horizon_ms", id: WorkerYouTubeDelivery,
			horizon: func(p *AlarmWorkerProfile) *int64 { return &p.YouTubeDelivery.HandoffHorizonMS },
		},
	} {
		t.Run(worker.name, func(t *testing.T) {
			settingstest.UseProfileFixture(t, "testdata/stack-worker-profile-alarm-worker.json")

			profile, err := LoadWorkerProfile()
			if err != nil {
				t.Fatal(err)
			}

			attempt := *profile.Loaded.Profile.Workers[worker.id].Executor.AttemptTimeout.Milliseconds

			for _, tc := range []struct {
				name      string
				horizonMS int64
				valid     bool
			}{
				{name: "missing horizon", horizonMS: 0},
				{name: "horizon one millisecond below attempt timeout", horizonMS: attempt - 1},
				{name: "horizon equals attempt timeout", horizonMS: attempt, valid: true},
				{name: "horizon covers queued burst", horizonMS: 300_000, valid: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					*worker.horizon(profile) = tc.horizonMS

					err := validateWorkerProfile(profile)
					if tc.valid && err != nil {
						t.Fatalf("validateWorkerProfile() error = %v, want horizon accepted", err)
					}

					if !tc.valid && (err == nil || !strings.Contains(strings.ReplaceAll(err.Error(), ".", " "), worker.key)) {
						t.Fatalf("validateWorkerProfile() error = %v, want %s rejected", err, worker.key)
					}
				})
			}
		})
	}
}

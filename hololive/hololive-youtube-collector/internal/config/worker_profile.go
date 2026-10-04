package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

// CollectionWorkerSettings는 collection worker profile의 service-owned settings 원문이다.
// DecodeWorkerSettings가 키의 정확한 일치를 요구하므로 JSON 태그가 profile wire 계약이다.
type CollectionWorkerSettings struct {
	AcquisitionBatch           int   `json:"acquisition_batch"`
	AcquisitionCadenceMS       int64 `json:"acquisition_cadence_ms"`
	LeaseTTLMS                 int64 `json:"lease_ttl_ms"`
	RenewIntervalMS            int64 `json:"renew_interval_ms"`
	RenewTimeoutMS             int64 `json:"renew_timeout_ms"`
	DBTimeoutMS                int64 `json:"db_timeout_ms"`
	CleanupTimeoutMS           int64 `json:"cleanup_timeout_ms"`
	ProviderAdmissionTimeoutMS int64 `json:"provider_admission_timeout_ms"`
	CollectionOverheadMS       int64 `json:"collection_overhead_ms"`
	PublishTimeoutMS           int64 `json:"publish_timeout_ms"`
	RetryMinMS                 int64 `json:"retry_min_ms"`
	RetryMaxMS                 int64 `json:"retry_max_ms"`
	ReleaseJitterMinMS         int64 `json:"release_jitter_min_ms"`
	ReleaseJitterMaxMS         int64 `json:"release_jitter_max_ms"`
	HolodexMaxInflight         int   `json:"holodex_max_inflight"`
	OfficialMaxInflight        int   `json:"official_max_inflight"`
	YouTubeJSMaxInflight       int   `json:"youtubejs_max_inflight"`
}

// WorkerProfile은 youtube-collector의 검증된 Stack Worker Profile이다.
// LoadWorkerProfile만 만들며, 반환된 값은 profile 소유 수치 정책까지 통과한 상태다.
type WorkerProfile struct {
	Loaded     workercontract.LoadedProfile
	Collection CollectionWorkerSettings
}

// LoadWorkerProfile은 STACK_WORKER_PROFILE_FILE의 collector profile을 읽고 profile이 소유한 정책 전체를 검증한다.
// DB·provider env를 읽지 않으므로 --check-worker-profile preflight가 운영 env 없이 같은 수치 경계를 적용한다.
func LoadWorkerProfile() (*WorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv("hololive", "youtube-collector")
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &WorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettings(loaded, "collection", &profile.Collection); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate collector worker profile: %w", err)
	}

	var policy Config

	policy.applyWorkerProfile(profile)

	if err := policy.validateProfilePolicy(); err != nil {
		return nil, fmt.Errorf("validate collector worker profile policy: %w", err)
	}

	return profile, nil
}

// validateWorkerProfile은 shape와 원시 밀리초 범위를 검증한다. 통과한 profile만 applyWorkerProfile이
// time.Duration으로 바꾸므로 PositiveValueProblems의 30일 상한이 변환 overflow를 막는다.
func validateWorkerProfile(profile *WorkerProfile) error {
	if profile == nil {
		return errors.New("youtube collector worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		"collection": {AttemptTimeout: workercontract.DurationModePerJob, Capacity: workercontract.CapacityModeBounded, MaxAge: workercontract.DurationModeFixed},
	})
	positive := map[string]int64{
		"collection.acquisition_cadence_ms":        profile.Collection.AcquisitionCadenceMS,
		"collection.lease_ttl_ms":                  profile.Collection.LeaseTTLMS,
		"collection.renew_interval_ms":             profile.Collection.RenewIntervalMS,
		"collection.renew_timeout_ms":              profile.Collection.RenewTimeoutMS,
		"collection.db_timeout_ms":                 profile.Collection.DBTimeoutMS,
		"collection.cleanup_timeout_ms":            profile.Collection.CleanupTimeoutMS,
		"collection.provider_admission_timeout_ms": profile.Collection.ProviderAdmissionTimeoutMS,
		"collection.collection_overhead_ms":        profile.Collection.CollectionOverheadMS,
		"collection.publish_timeout_ms":            profile.Collection.PublishTimeoutMS,
		"collection.retry_min_ms":                  profile.Collection.RetryMinMS,
		"collection.retry_max_ms":                  profile.Collection.RetryMaxMS,
		"collection.release_jitter_min_ms":         profile.Collection.ReleaseJitterMinMS,
		"collection.release_jitter_max_ms":         profile.Collection.ReleaseJitterMaxMS,
	}

	problems = append(problems, runtimepolicy.PositiveValueProblems(positive)...)

	worker := workers["collection"]

	problems = append(problems, capacityProblems(profile, &worker)...)
	problems = append(problems, concurrencyProblems(profile, &worker)...)
	problems = append(problems, timingProblems(profile)...)

	if err := runtimepolicy.JoinWorkerProfileProblems("youtube-collector", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func capacityProblems(profile *WorkerProfile, worker *workercontract.WorkerProfile) []string {
	capacity := int64(0)

	if worker != nil && worker.Queue.Capacity.Items != nil {
		capacity = *worker.Queue.Capacity.Items
	}

	if profile.Collection.AcquisitionBatch < 1 || int64(profile.Collection.AcquisitionBatch) > capacity {
		return []string{"collection acquisition batch must fit queue capacity"}
	}

	return nil
}

func concurrencyProblems(profile *WorkerProfile, worker *workercontract.WorkerProfile) []string {
	problems := make([]string, 0)

	if worker == nil {
		return []string{"collection worker is missing"}
	}

	for name, value := range map[string]int{
		"holodex_max_inflight":   profile.Collection.HolodexMaxInflight,
		"official_max_inflight":  profile.Collection.OfficialMaxInflight,
		"youtubejs_max_inflight": profile.Collection.YouTubeJSMaxInflight,
	} {
		if value < 1 || value > worker.Executor.ConfiguredWorkers {
			problems = append(problems, "collection "+name+" must be within configured workers")
		}
	}

	return problems
}

func timingProblems(profile *WorkerProfile) []string {
	problems := make([]string, 0)

	if profile.Collection.RenewIntervalMS+profile.Collection.RenewTimeoutMS+1000 >= profile.Collection.LeaseTTLMS {
		problems = append(problems, "collection renewal budget must fit lease TTL")
	}

	if profile.Collection.RetryMaxMS < profile.Collection.RetryMinMS || profile.Collection.ReleaseJitterMaxMS < profile.Collection.ReleaseJitterMinMS {
		problems = append(problems, "collection retry or jitter range is invalid")
	}

	return problems
}

// applyWorkerProfile은 validateWorkerProfile을 통과한 profile의 정책 값을 Config로 옮긴다.
// 형태 검증이 bounded capacity와 fixed max_age를 보장하므로 두 포인터는 nil이 아니다.
func (c *Config) applyWorkerProfile(profile *WorkerProfile) {
	worker := profile.Loaded.Profile.Workers["collection"]
	collection := profile.Collection

	c.TotalWorkers = worker.Executor.ConfiguredWorkers
	c.QueueCapacity = int(*worker.Queue.Capacity.Items)
	c.QueueMaxAge = millisecondsDuration(*worker.Queue.MaxAge.Milliseconds)
	c.AcquisitionBatch = collection.AcquisitionBatch
	c.AcquisitionCadence = millisecondsDuration(collection.AcquisitionCadenceMS)
	c.LeaseTTL = millisecondsDuration(collection.LeaseTTLMS)
	c.RenewInterval = millisecondsDuration(collection.RenewIntervalMS)
	c.RenewTimeout = millisecondsDuration(collection.RenewTimeoutMS)
	c.DBTimeout = millisecondsDuration(collection.DBTimeoutMS)
	c.CleanupTimeout = millisecondsDuration(collection.CleanupTimeoutMS)
	c.ProviderAdmissionTimeout = millisecondsDuration(collection.ProviderAdmissionTimeoutMS)
	c.CollectionOverhead = millisecondsDuration(collection.CollectionOverheadMS)
	c.PublishTimeout = millisecondsDuration(collection.PublishTimeoutMS)
	c.RetryMin = millisecondsDuration(collection.RetryMinMS)
	c.RetryMax = millisecondsDuration(collection.RetryMaxMS)
	c.ReleaseJitterMin = millisecondsDuration(collection.ReleaseJitterMinMS)
	c.ReleaseJitterMax = millisecondsDuration(collection.ReleaseJitterMaxMS)
	c.HolodexMaxInflight = collection.HolodexMaxInflight
	c.OfficialMaxInflight = collection.OfficialMaxInflight
	c.YouTubeJSMaxInflight = collection.YouTubeJSMaxInflight
}

func millisecondsDuration(milliseconds int64) time.Duration {
	return time.Duration(milliseconds) * time.Millisecond
}

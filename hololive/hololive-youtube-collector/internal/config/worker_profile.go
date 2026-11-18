package config

import (
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/runtimepolicy"
)

const WorkerCollection = "collection"

func WorkerIdentity() workercontract.Identity {
	return workercontract.Identity{
		Service:   "hololive",
		Role:      "youtube-collector",
		WorkerIDs: []string{WorkerCollection},
	}
}

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

type WorkerProfile struct {
	Loaded     workercontract.LoadedProfile
	Collection CollectionWorkerSettings
}

func LoadWorkerProfile() (*WorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv(WorkerIdentity())
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	return DecodeWorkerProfile(loaded)
}

func DecodeWorkerProfile(loaded workercontract.LoadedProfile) (*WorkerProfile, error) {
	profile := &WorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettingsSet(loaded, map[string]any{WorkerCollection: &profile.Collection}); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate collector worker profile: %w", err)
	}

	policy := profile.Config()

	if err := policy.validateProfilePolicy(); err != nil {
		return nil, fmt.Errorf("validate collector worker profile policy: %w", err)
	}

	return profile, nil
}

func validateWorkerProfile(profile *WorkerProfile) error {
	if profile == nil {
		return errors.New("youtube collector worker profile is nil")
	}

	workers := profile.Loaded.Profile.Workers
	problems := workercontract.ShapeProblems(workers, map[string]workercontract.WorkerShape{
		WorkerCollection: {AttemptTimeout: workercontract.DurationModePerJob, Capacity: workercontract.CapacityModeBounded, MaxAge: workercontract.DurationModeFixed},
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

	worker := workers[WorkerCollection]

	problems = append(problems, capacityProblems(profile, &worker)...)

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

func (profile *WorkerProfile) Config() Config {
	config := DefaultConfig()
	config.applyWorkerProfile(profile)

	return config
}

func (c *Config) applyWorkerProfile(profile *WorkerProfile) {
	worker := profile.Loaded.Profile.Workers[WorkerCollection]
	collection := profile.Collection

	c.TotalWorkers = worker.Executor.ConfiguredWorkers
	c.QueueCapacity = int(*worker.Queue.Capacity.Items)
	c.QueueMaxAge, _ = worker.Queue.MaxAge.FixedDuration()
	c.AcquisitionBatch = collection.AcquisitionBatch
	c.AcquisitionCadence = workercontract.Milliseconds(collection.AcquisitionCadenceMS)
	c.LeaseTTL = workercontract.Milliseconds(collection.LeaseTTLMS)
	c.RenewInterval = workercontract.Milliseconds(collection.RenewIntervalMS)
	c.RenewTimeout = workercontract.Milliseconds(collection.RenewTimeoutMS)
	c.DBTimeout = workercontract.Milliseconds(collection.DBTimeoutMS)
	c.CleanupTimeout = workercontract.Milliseconds(collection.CleanupTimeoutMS)
	c.ProviderAdmissionTimeout = workercontract.Milliseconds(collection.ProviderAdmissionTimeoutMS)
	c.CollectionOverhead = workercontract.Milliseconds(collection.CollectionOverheadMS)
	c.PublishTimeout = workercontract.Milliseconds(collection.PublishTimeoutMS)
	c.RetryMin = workercontract.Milliseconds(collection.RetryMinMS)
	c.RetryMax = workercontract.Milliseconds(collection.RetryMaxMS)
	c.ReleaseJitterMin = workercontract.Milliseconds(collection.ReleaseJitterMinMS)
	c.ReleaseJitterMax = workercontract.Milliseconds(collection.ReleaseJitterMaxMS)
	c.HolodexMaxInflight = collection.HolodexMaxInflight
	c.OfficialMaxInflight = collection.OfficialMaxInflight
	c.YouTubeJSMaxInflight = collection.YouTubeJSMaxInflight
}

package collector

import (
	"errors"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/internal/load"
)

func LoadWorkerProfile() (*settings.YouTubeCollectorWorkerProfile, error) {
	loaded, err := workercontract.LoadProfileFromEnv("hololive", "youtube-collector")
	if err != nil {
		return nil, fmt.Errorf("load stack worker profile: %w", err)
	}

	profile := &settings.YouTubeCollectorWorkerProfile{Loaded: loaded}
	if err := workercontract.DecodeWorkerSettings(loaded, "collection", &profile.Collection); err != nil {
		return nil, fmt.Errorf("decode worker settings: %w", err)
	}

	if err := validateWorkerProfile(profile); err != nil {
		return nil, fmt.Errorf("validate collector worker profile: %w", err)
	}

	return profile, nil
}

func validateWorkerProfile(profile *settings.YouTubeCollectorWorkerProfile) error {
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

	problems = append(problems, load.PositiveValueProblems(positive)...)

	worker := workers["collection"]

	problems = append(problems, capacityProblems(profile, &worker)...)
	problems = append(problems, concurrencyProblems(profile, &worker)...)
	problems = append(problems, timingProblems(profile)...)

	if err := load.JoinWorkerProfileProblems("youtube-collector", problems); err != nil {
		return fmt.Errorf("join worker profile problems: %w", err)
	}

	return nil
}

func capacityProblems(profile *settings.YouTubeCollectorWorkerProfile, worker *workercontract.WorkerProfile) []string {
	capacity := int64(0)

	if worker != nil && worker.Queue.Capacity.Items != nil {
		capacity = *worker.Queue.Capacity.Items
	}

	if profile.Collection.AcquisitionBatch < 1 || int64(profile.Collection.AcquisitionBatch) > capacity {
		return []string{"collection acquisition batch must fit queue capacity"}
	}

	return nil
}

func concurrencyProblems(profile *settings.YouTubeCollectorWorkerProfile, worker *workercontract.WorkerProfile) []string {
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

func timingProblems(profile *settings.YouTubeCollectorWorkerProfile) []string {
	problems := make([]string, 0)

	if profile.Collection.RenewIntervalMS+profile.Collection.RenewTimeoutMS+1000 >= profile.Collection.LeaseTTLMS {
		problems = append(problems, "collection renewal budget must fit lease TTL")
	}

	if profile.Collection.RetryMaxMS < profile.Collection.RetryMinMS || profile.Collection.ReleaseJitterMaxMS < profile.Collection.ReleaseJitterMinMS {
		problems = append(problems, "collection retry or jitter range is invalid")
	}

	return problems
}

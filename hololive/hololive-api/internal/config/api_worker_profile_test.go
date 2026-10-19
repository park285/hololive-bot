package config

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/park285/shared-go/v2/pkg/irisdurable"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestDisabledSourceObservationProfileRemainsValid(t *testing.T) {
	loaded := settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")
	worker := loaded.Profile.Workers["source_observation"]

	worker.Executor.Enabled = false
	loaded.Profile.Workers["source_observation"] = worker

	contents, err := jsonv2.Marshal(loaded.Profile)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "disabled-source-observation.json")
	if writeErr := os.WriteFile(path, contents, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	t.Setenv(workercontract.ProfileFileEnv, path)

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	config := DefaultYouTubePlaneConfig()
	applySourceObservationWorkerProfile(&config, profile)

	if config.Enabled {
		t.Fatal("disabled profile enabled the source observation executor")
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("disabled runtime config: %v", err)
	}
}

func TestStackWorkerProfileIsRequired(t *testing.T) {
	settingstest.UnsetEnv(t, workercontract.ProfileFileEnv)

	if _, err := LoadAPIWorkerProfile(); err == nil {
		t.Fatalf("LoadAPIWorkerProfile() error = %v", err)
	}
}

func TestLoadAPIWorkerProfileSourceObservationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*SourceObservationWorkerSettings, *workercontract.WorkerProfile)
		valid  bool
	}{
		{name: "exact lease and shutdown budgets", valid: true},
		{name: "audit timeout and lease", mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			s.ClaimLeaseMS = 30000
			w.Executor.AttemptTimeout.Milliseconds = new(int64(90000))
		}},
		{name: "transaction below minimum", mutate: func(_ *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(999))
		}},
		{name: "transaction at minimum", valid: true, mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(1000))
			s.ClaimLeaseMS, s.ShutdownTimeoutMS = 14000, 2000
		}},
		{name: "transaction at maximum", valid: true, mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(60000))
			s.ClaimLeaseMS, s.ShutdownTimeoutMS = 250000, 120000
		}},
		{name: "transaction above maximum", mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(60001))
			s.ClaimLeaseMS, s.ShutdownTimeoutMS = 250004, 120002
		}},
		{name: "lease below budget", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) { s.ClaimLeaseMS-- }},
		{name: "shutdown below budget", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) { s.ShutdownTimeoutMS-- }},
		{name: "zero claim interval", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) { s.ClaimIntervalMS = 0 }},
		{name: "zero batch", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) { s.ClaimBatchSize = 0 }},
		{name: "batch at minimum", valid: true, mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			s.ClaimBatchSize, s.ClaimLeaseMS, w.Executor.ConfiguredWorkers = 1, 20000, 1
		}},
		{name: "batch below workers", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) { s.ClaimBatchSize = 1 }},
		{name: "batch at maximum", valid: true, mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) {
			s.ClaimBatchSize, s.ClaimLeaseMS = 100, 1010000
		}},
		{name: "batch above maximum", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) {
			s.ClaimBatchSize, s.ClaimLeaseMS = 101, 1020000
		}},
		{name: "workers at maximum", valid: true, mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.ConfiguredWorkers = 16
			s.DBOperationConcurrency, s.ClaimBatchSize, s.ClaimLeaseMS = 16, 16, 170000
		}},
		{name: "workers above maximum", mutate: func(s *SourceObservationWorkerSettings, w *workercontract.WorkerProfile) {
			w.Executor.ConfiguredWorkers = 17
			s.DBOperationConcurrency, s.ClaimBatchSize, s.ClaimLeaseMS = 17, 17, 180000
		}},
		{name: "workers exceed DB budget", mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) {
			s.DBOperationConcurrency = 1
		}},
		{name: "pool reservation remains runtime owned", valid: true, mutate: func(s *SourceObservationWorkerSettings, _ *workercontract.WorkerProfile) {
			s.DBOperationConcurrency = 4
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				checkSourceObservationBoundary(t, enabled, tc.valid, tc.mutate)
			}
		})
	}
}

func TestSourceObservationPoolReservationRemainsRuntimeOwned(t *testing.T) {
	settingstest.UseProfileFixture(t, "stack-worker-profile-api.json")

	profile, err := LoadAPIWorkerProfile()
	if err != nil {
		t.Fatal(err)
	}

	profile.SourceObservation.DBOperationConcurrency = 4
	if err := validateAPIWorkerProfile(profile); err != nil {
		t.Fatalf("profile-only validation must not assume a pool size: %v", err)
	}

	config := DefaultYouTubePlaneConfig()
	applySourceObservationWorkerProfile(&config, profile)

	config.PostgresPoolMaxConns = 4
	if err := config.Validate(); err == nil {
		t.Fatal("runtime accepted DB concurrency without a reserved connection")
	}

	config.PostgresPoolMaxConns = 5
	if err := config.Validate(); err != nil {
		t.Fatalf("runtime rejected one reserved connection: %v", err)
	}
}

func TestAPIWorkerProfileDurableBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		workerID string
		mutate   func(map[string]any, *workercontract.WorkerProfile)
		valid    bool
	}{
		{name: "retention equal admission", workerID: WorkerBotWebhookInbox, mutate: func(s map[string]any, _ *workercontract.WorkerProfile) {
			s["terminal_retention_ms"] = irisdurable.IrisReplyAdmissionRetention.Milliseconds()
		}},
		{name: "retention beyond admission", workerID: WorkerBotWebhookInbox, valid: true, mutate: func(s map[string]any, _ *workercontract.WorkerProfile) {
			s["terminal_retention_ms"] = irisdurable.IrisReplyAdmissionRetention.Milliseconds() + 1
		}},
		{name: "heartbeat equal command stale", workerID: WorkerBotWebhookInbox, mutate: func(s map[string]any, _ *workercontract.WorkerProfile) {
			s["heartbeat_interval_ms"] = BotCommandStaleAfter.Milliseconds()
			s["claim_lease_ms"] = 600000
		}},
		{name: "reply timeout equal lease", workerID: WorkerBotReplyOutbox, mutate: func(_ map[string]any, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(120000))
		}},
		{name: "reply timeout below lease", workerID: WorkerBotReplyOutbox, valid: true, mutate: func(_ map[string]any, w *workercontract.WorkerProfile) {
			w.Executor.AttemptTimeout.Milliseconds = new(int64(119999))
		}},
		{name: "obsolete reply budget", workerID: WorkerBotReplyOutbox, mutate: func(s map[string]any, _ *workercontract.WorkerProfile) {
			s["dispatch_budget_ms"] = 105000
		}},
		{name: "obsolete source budget", workerID: WorkerSourceObservation, mutate: func(s map[string]any, _ *workercontract.WorkerProfile) {
			s["transaction_timeout_ms"] = 10000
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, enabled := range []bool{true, false} {
				loaded := settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")
				worker := loaded.Profile.Workers[tc.workerID]

				var settings map[string]any

				if err := jsonv2.Unmarshal(worker.Settings, &settings); err != nil {
					t.Fatal(err)
				}

				worker.Executor.Enabled = enabled
				tc.mutate(settings, &worker)

				useAPIWorkerSettings(t, loaded, tc.workerID, worker, settings)

				_, err := LoadAPIWorkerProfile()
				if (err == nil) != tc.valid {
					t.Fatalf("enabled=%v error=%v, want valid=%v", enabled, err, tc.valid)
				}
			}
		})
	}
}

func useAPIWorkerSettings(t *testing.T, loaded workercontract.LoadedProfile, workerID string, worker workercontract.WorkerProfile, settings any) {
	t.Helper()

	contents, err := jsonv2.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}

	worker.Settings = contents
	loaded.Profile.Workers[workerID] = worker

	contents, err = jsonv2.Marshal(loaded.Profile)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "profile.json")
	if writeErr := os.WriteFile(path, contents, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	t.Setenv(workercontract.ProfileFileEnv, path)
}

func checkSourceObservationBoundary(t *testing.T, enabled, valid bool, mutate func(*SourceObservationWorkerSettings, *workercontract.WorkerProfile)) {
	t.Helper()

	loaded := settingstest.LoadProfileFixture(t, "hololive", "api", "stack-worker-profile-api.json")
	worker := loaded.Profile.Workers[WorkerSourceObservation]
	settings := SourceObservationWorkerSettings{
		DBOperationConcurrency: 3,
		ClaimBatchSize:         4,
		ClaimIntervalMS:        2000,
		ClaimLeaseMS:           50000,
		ShutdownTimeoutMS:      20000,
	}

	worker.Executor.Enabled = enabled

	if mutate != nil {
		mutate(&settings, &worker)
	}

	useAPIWorkerSettings(t, loaded, WorkerSourceObservation, worker, settings)

	profile, err := LoadAPIWorkerProfile()

	if !valid {
		if err == nil {
			t.Fatalf("LoadAPIWorkerProfile() enabled=%v accepted an invalid worker boundary", enabled)
		}

		return
	}

	if err != nil {
		t.Fatalf("LoadAPIWorkerProfile() enabled=%v rejected a valid worker boundary: %v", enabled, err)
	}

	config := DefaultYouTubePlaneConfig()
	applySourceObservationWorkerProfile(&config, profile)

	want, _ := worker.Executor.AttemptTimeout.FixedDuration()
	if config.TransactionTimeout != want {
		t.Fatalf("transaction budget = %s, want executor timeout %s", config.TransactionTimeout, want)
	}
}

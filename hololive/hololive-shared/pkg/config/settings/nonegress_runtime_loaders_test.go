package settings

import (
	"testing"
)

func TestNonEgressConfigLoadersSkipWorkerProfileFetchWithAccidentalIrisToken(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T)
		load  func() (*Config, error)
	}{
		{
			name: "admin api",
			setup: func(t *testing.T) {
				t.Helper()
				setAdminAPIRuntimeEnv(t)
			},
			load: LoadAdminAPIRuntime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			t.Setenv(irisBotTokenEnv, "accidental-egress-token")
			t.Setenv("IRIS_BASE_URL", "http://iris.invalid")

			cfg, err := tt.load()
			if err != nil {
				t.Fatalf("%s load error = %v, want nil without Iris worker profile fetch", tt.name, err)
			}

			if cfg.Webhook.WorkerCount != 0 || cfg.Webhook.QueueSize != 0 {
				t.Fatalf("%s Webhook = %#v, want unused zero value", tt.name, cfg.Webhook)
			}

			if cfg.APIWorkerProfile != nil || cfg.AlarmWorkerProfile != nil {
				t.Fatalf("%s unexpectedly loaded a worker profile", tt.name)
			}
		})
	}
}

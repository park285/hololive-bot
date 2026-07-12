package settings

import (
	"strings"
	"testing"
	"time"
)

func TestFetchIrisBotWebhookWorkerProfileRejectsProducerHashMismatch(t *testing.T) {
	server := newIrisRuntimeDiagnosticsServer(t, `{
			"workers":{"webhook":{"webhookPipeline":{
				"profileEnabled":true,
				"profileVersion":1,
				"profileId":"prod-standard-2026-05-26",
				"profileHash":"5f4bb7659f48a6064e959f6985b0996d7f9cb9f9866d1c47bad98a416f6f6994",
				"workerProfile":{
					"version":1,
					"profile_id":"prod-standard-2026-05-26",
					"delivery":{"lane_workers":32,"lane_queue_capacity":128,"max_global_in_flight":32,"max_per_endpoint_in_flight":8,"max_drain_per_tick":128,"max_attempts":6,"request_timeout_ms":125000,"lane_idle_timeout_ms":750,"breaker_failure_threshold":5,"breaker_cooldown_ms":30000},
					"receive":{"workers":16,"queue_size":1000,"enqueue_timeout_ms":50,"handler_timeout_ms":120000,"max_body_bytes":65536,"dedup_ttl_ms":60000,"dedup_timeout_ms":200},
					"bot_pool":{"workers":10,"queue_size":100},
					"validation":{"min_queue_per_endpoint_multiplier":4,"require_receive_capacity_for_endpoint_burst":true}
				}
			}}}
		}`)
	t.Setenv("IRIS_TRANSPORT", "http1")
	t.Setenv("IRIS_BASE_URL_ALLOWED_HOSTS", testURLHostname(t, server.URL))
	config := IrisConfig{
		BaseURL:                   server.URL,
		BotToken:                  "test-token",
		HTTPTimeout:               time.Second,
		HTTPDialTimeout:           time.Second,
		HTTPResponseHeaderTimeout: time.Second,
	}

	_, err := fetchIrisBotWebhookWorkerProfile(&config)
	if err == nil {
		t.Fatal("fetchIrisBotWebhookWorkerProfile() error = nil, want producer hash mismatch")
	}
	if !strings.Contains(err.Error(), "profileHash") {
		t.Fatalf("fetchIrisBotWebhookWorkerProfile() error = %v", err)
	}
}

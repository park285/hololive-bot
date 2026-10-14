package collectorruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

// DB 장애 동안 후보 탐색은 1초마다 실패한다. 실패 WARN은 첫 건과 1분 간격 요약만 남기고, 매 cycle 실패는 counter가
// 센다. 복구되면 INFO 한 건으로 연속 실패 기간을 닫는다.
func TestDiscoveryFailureLogIsThrottledWhileCounterCountsEveryCycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		scheduler := newLifecycleScheduler(t)
		registerer := prometheus.NewPedanticRegistry()

		scheduler.metrics = NewMetrics(registerer)

		var logged bytes.Buffer

		scheduler.logger = slog.New(slog.NewJSONHandler(&logged, nil))

		source := newEmptyCandidateStub(t)

		source.err = fmt.Errorf("list collection job candidates: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
		scheduler.candidates = source

		const failedCycles = 121

		for range failedCycles {
			scheduler.discoverOnce(t.Context())
			// 운영 기본 acquisition cadence(1초)로 cycle 간격을 맞춘다.
			time.Sleep(time.Second)
		}

		source.mu.Lock()

		source.err = nil

		source.mu.Unlock()
		scheduler.discoverOnce(t.Context())

		records := decodeLogRecords(t, &logged)
		warns := recordsWithMessage(records, "YouTube collection candidate discovery failed")
		recovered := recordsWithMessage(records, "YouTube collection candidate discovery recovered")

		if len(warns) != 3 || len(recovered) != 1 || len(records) != 4 {
			t.Fatalf("records = %d, warns = %d, recovered = %d, want 4/3/1:\n%s", len(records), len(warns), len(recovered), logged.String())
		}

		for index, wantSuppressed := range []float64{0, 59, 59} {
			warn := warns[index]
			if warn["level"] != "WARN" || warn["suppressed_cycles"] != wantSuppressed || warn["error_class"] != string(contract.ClassTransient) ||
				warn["phase"] != "candidate_load" || warn["job_kind"] == nil {
				t.Fatalf("warn[%d] = %v, want suppressed_cycles=%v with runner scope", index, warn, wantSuppressed)
			}

			if _, hasJobKey := warn["job_key"]; hasJobKey {
				t.Fatalf("warn[%d] carries empty job fields: %v", index, warn)
			}
		}

		if recovered[0]["level"] != "INFO" || recovered[0]["failed_cycles"] != float64(failedCycles) {
			t.Fatalf("recovered = %v, want failed_cycles=%d", recovered[0], failedCycles)
		}

		if got := metricValue(t, registerer, "youtube_collection_discovery_cycles_total", map[string]string{labelResult: discoveryCycleCandidateLoadFailed}); got != failedCycles {
			t.Fatalf("failed discovery cycles = %v, want %d", got, failedCycles)
		}

		if got := metricValue(t, registerer, "youtube_collection_discovery_cycles_total", map[string]string{labelResult: discoveryCycleOK}); got != 1 {
			t.Fatalf("ok discovery cycles = %v, want 1", got)
		}
	})
}

// 종료로 scheduler context가 취소된 뒤의 취소는 실패 WARN이 아니라 INFO다. 바깥 context가 살아 있는 취소(renew 실패 등)와
// 취소가 아닌 실패는 그대로 WARN이다.
func TestShutdownCancellationIsLoggedAsInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		shutdown    bool
		class       contract.FailureClass
		wantLevel   string
		wantMessage string
	}{
		{name: "shutdown cancel", shutdown: true, class: contract.ClassCanceled, wantLevel: "INFO", wantMessage: "YouTube collection job canceled by shutdown"},
		{name: "inner cancel while running", class: contract.ClassCanceled, wantLevel: "WARN", wantMessage: "YouTube collection job failed"},
		{name: "transient failure during shutdown", shutdown: true, class: contract.ClassTransient, wantLevel: "WARN", wantMessage: "YouTube collection job failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if test.shutdown {
				cancel()
			}

			var logged bytes.Buffer

			executor := &collectionExecutor{logger: slog.New(slog.NewJSONHandler(&logged, nil))}
			spec := joblease.JobSpec{JobKey: testJobKey, Provider: contract.ProviderYouTubeJS, CollectionJobKind: testCommunityJobKind}
			executor.logFailure(ctx, "acquire", "lease_acquire_failed", string(test.class), "begin pgx transaction: context canceled", &spec, &contract.LeaseProof{})

			records := decodeLogRecords(t, &logged)
			if len(records) != 1 || records[0]["level"] != test.wantLevel || records[0]["msg"] != test.wantMessage ||
				records[0]["job_key"] != testJobKey || records[0]["error_class"] != string(test.class) {
				t.Fatalf("records = %v, want one %s %q", records, test.wantLevel, test.wantMessage)
			}
		})
	}
}

func TestDiscoveryShutdownCancellationIsInfoAndOpensNoStreak(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)

	var logged bytes.Buffer

	scheduler.logger = slog.New(slog.NewJSONHandler(&logged, nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	scheduler.reportDiscoveryFailure(ctx, globalDiscoveryKey, nil, fmt.Errorf("load current projection: %w", context.Canceled))

	records := decodeLogRecords(t, &logged)
	if len(records) != 1 || records[0]["level"] != "INFO" || records[0]["msg"] != "YouTube collection candidate discovery canceled by shutdown" {
		t.Fatalf("records = %v, want one shutdown INFO", records)
	}

	if len(scheduler.discoveryFailures) != 0 {
		t.Fatalf("shutdown cancellation opened a failure streak: %v", scheduler.discoveryFailures)
	}
}

func decodeLogRecords(t *testing.T, logged *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any

	for line := range strings.Lines(logged.String()) {
		var record map[string]any

		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}

		records = append(records, record)
	}

	return records
}

func recordsWithMessage(records []map[string]any, message string) []map[string]any {
	var out []map[string]any

	for _, record := range records {
		if record["msg"] == message {
			out = append(out, record)
		}
	}

	return out
}

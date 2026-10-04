package collectorruntime

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

func TestContentTabRPCDistinguishesMissingTabsFromParserDrift(t *testing.T) {
	tests := []struct {
		name      string
		driftTab  string
		wantCalls int32
	}{
		{name: "genuine missing tabs complete empty", wantCalls: 2},
		{name: "videos parser drift defers", driftTab: "videos", wantCalls: 1},
		{name: "shorts parser drift discards collected videos", driftTab: "shorts", wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				fatal []error
				calls atomic.Int32
			)

			client := contentTabRPCFixture(t, tt.driftTab, &calls)
			executor, spec, pool := contentTabExecutorFixture(t, client, &fatal)
			executor.runSpec(t.Context(), spec)

			if got := calls.Load(); got != tt.wantCalls {
				t.Fatalf("helper calls = %d, want %d", got, tt.wantCalls)
			}

			if len(fatal) != 0 {
				t.Fatalf("tab response stopped the collector: %v", fatal)
			}

			assertContentTabTerminal(t, pool, spec.JobKey, tt.driftTab != "")

			attempts := executor.workerTotals.Snapshot().Attempts
			if tt.driftTab == "" {
				if attempts.Success != 1 || attempts.Failed != 0 || !executor.readiness.Snapshot().collectionSuccess {
					t.Fatalf("missing tabs did not complete successfully: %+v", attempts)
				}
			} else if attempts.Success != 0 || attempts.Failed != 1 || executor.readiness.Snapshot().collectionSuccess {
				t.Fatalf("parser drift was counted as success: %+v", attempts)
			}
		})
	}
}

func contentTabRPCFixture(t *testing.T, driftTab string, calls *atomic.Int32) *youtubejs.RPC {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)

		if r.Method != http.MethodPost || r.URL.Path != "/v1/content" {
			t.Errorf("unexpected helper request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		var request youtubejs.ContentRequest

		if err := jsonv2.UnmarshalRead(r.Body, &request); err != nil {
			t.Errorf("decode content request: %v", err)
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		w.Header().Set("Content-Type", "application/json")

		if request.Kind == driftTab {
			w.WriteHeader(http.StatusUnprocessableEntity)

			if _, err := w.Write([]byte(`{"protocol_version":1,"error":{"code":"parser_drift","class":"DATA_CONTRACT","retry":{"kind":"default"},"message":"channel tab parser failed"}}`)); err != nil {
				t.Errorf("write parser drift response: %v", err)
			}

			return
		}

		result := youtubejs.ContentResult{
			ProtocolVersion:   youtubejs.ProtocolVersion,
			PageCount:         1,
			Exhausted:         true,
			Continuity:        string(contract.ContinuityNotApplicable),
			TerminationReason: youtubejs.TerminationExhausted,
			Items:             []youtubejs.ContentItem{},
			MissingTab:        driftTab == "",
		}
		if !result.MissingTab {
			result.Continuity = string(contract.ContinuityContiguous)
			result.Items = []youtubejs.ContentItem{{VideoID: "vid-a", ChannelID: testSubjectKey, Title: "Video"}}
		}

		if err := jsonv2.MarshalWrite(w, result); err != nil {
			t.Errorf("write content response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return youtubejs.NewRPC(server.Client(), server.URL, nil)
}

func contentTabExecutorFixture(t *testing.T, client *youtubejs.RPC, fatal *[]error) (*collectionExecutor, *joblease.JobSpec, *pgxpool.Pool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	seedRuntimeTargets(t, pool, []leaseSeed{
		{testSubjectKey, contract.KindVideoList},
		{testSubjectKey, contract.KindShortsList},
	})

	executor := newRunErrorExecutor(fatal)

	executor.retryBounds = testRetryBounds

	config := runtimeLeaseConfig()

	var err error

	executor.repository, err = joblease.NewRepository(pool, &config)
	if err != nil {
		t.Fatal(err)
	}

	executor.registry, err = newTestRegistry(withOverride(youtubejscollector.NewContentRunner(client, sourceobservation.NewRepository(pool), time.Second))...)
	if err != nil {
		t.Fatal(err)
	}

	executor.publisher = NewPublisher(pool)
	executor.owner = testOwnerInstance
	executor.gates = defaultProviderGates()
	executor.readiness = &readinessTracker{}
	executor.workerTracker = workercontract.NewExecutorTracker()
	executor.workerTotals = &workercontract.Counters{}

	return executor, &joblease.JobSpec{
		JobKey: "collector:youtubejs:youtubejs_content:UC_TEST", Provider: contract.ProviderYouTubeJS, Class: testJobClassSubject,
		CollectionJobKind: "youtubejs_content", SubjectKey: testSubjectKey, PollInterval: time.Minute,
	}, pool
}

func assertContentTabTerminal(t *testing.T, pool *pgxpool.Pool, jobKey string, drift bool) {
	t.Helper()

	var (
		state, code, class string
		observations       int
	)

	if err := pool.QueryRow(t.Context(), `
		SELECT slot_state, COALESCE(last_failure_code, ''), COALESCE(last_failure_class, '')
		FROM youtube_collection_job_leases WHERE job_key = $1
	`, jobKey).Scan(&state, &code, &class); err != nil {
		t.Fatal(err)
	}

	if drift {
		if state != "DEFERRED" || code != string(collecterr.ParserDrift) || class != string(collecterr.ClassDataContract) {
			t.Fatalf("parser drift terminal = %s %s/%s, want DEFERRED parser_drift/DATA_CONTRACT", state, code, class)
		}
	} else if state != "IDLE" || code != "" || class != "" {
		t.Fatalf("missing tabs terminal = %s %s/%s, want successful completion", state, code, class)
	}

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM source_observations`).Scan(&observations); err != nil {
		t.Fatal(err)
	}

	if observations != 0 {
		t.Fatalf("published observations = %d, want 0", observations)
	}
}

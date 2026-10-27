package runtime

import (
	"context"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestDisabledRuntimeOnlyRunsAndJoinsQueueSampler(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var samples, claims, consumes, closes atomic.Int64

		runtime := newTestRuntime(t, fakeClaimer{
			claim: func(context.Context, sourceobservation.ClaimOptions) (sourceobservation.ClaimedBatch, error) {
				claims.Add(1)

				return sourceobservation.ClaimedBatch{}, nil
			},
		}, fakeConsumer{consume: func(context.Context, sourceobservation.Claim) error {
			consumes.Add(1)

			return nil
		}})

		runtime.Config.Enabled = false
		runtime.Config.Replay.Enabled = true
		runtime.Config.LiveEndFinalizer.Enabled = true
		runtime.workerTracker = workercontract.NewExecutorTracker()
		runtime.workerTotals = &workercontract.Counters{}
		runtime.closePool = func() { closes.Add(1) }
		runtime.workerSampler = workercontract.NewQueueSampler(func(context.Context) (workercontract.QueueValues, error) {
			samples.Add(1)

			return workercontract.QueueValues{Depth: 7, OldestQueuedAge: time.Minute}, nil
		})
		t.Cleanup(runtime.Close)

		// 비활성 준비 경로는 획득 상태 조회나 프로젝션 갱신을 하지 않아야 한다.
		runtime.refresher = nil
		require.NoError(t, runtime.prepare(t.Context()))

		errCh := make(chan error, 1)
		runtime.Start(t.Context(), errCh)
		synctest.Wait()
		require.Equal(t, int64(1), samples.Load(), "sample immediately without starting executors")
		require.Equal(t, int64(7), *runtime.WorkerRegistration().QueueSnapshot().Depth)
		require.Zero(t, runtime.WorkerRegistration().ExecutorSnapshot().RunningWorkers)
		require.Zero(t, runtime.WorkerRegistration().ExecutorSnapshot().InFlight)

		time.Sleep(workercontract.QueueSampleInterval)
		synctest.Wait()
		require.Equal(t, int64(2), samples.Load())
		require.Zero(t, claims.Load())
		require.Zero(t, consumes.Load())
		require.Zero(t, runtime.loopCount, "disabled source observation must not start maintenance loops")

		select {
		case err := <-errCh:
			t.Fatalf("disabled runtime started an executor loop: %v", err)
		default:
		}

		require.NoError(t, runtime.Shutdown(t.Context()))

		stoppedSamples := samples.Load()

		time.Sleep(2 * workercontract.QueueSampleInterval)
		synctest.Wait()
		require.Equal(t, stoppedSamples, samples.Load(), "shutdown must join the sampler")
		require.NoError(t, runtime.CloseContext(t.Context()))
		require.Equal(t, int64(1), closes.Load())
	})
}

func TestDisabledRuntimeReportsDurableQueueDepth(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedFailureRecoveryQueue(t, pool)

	config := testYouTubePlaneConfig(t)

	config.Enabled = false

	runtime, err := newRuntime(&config, slog.New(slog.DiscardHandler), pool, nil)
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	require.NoError(t, runtime.prepare(t.Context()))
	runtime.Start(t.Context(), make(chan error, 1))
	require.Eventually(t, func() bool {
		return runtime.WorkerRegistration().QueueSnapshot().Status == workercontract.QueueSnapshotCurrent
	}, 5*time.Second, time.Millisecond)

	// 나머지 API 큐 둘은 봇 런타임 소유이므로 이 실행 영역의 등록만 격리한다.
	loaded := settingstest.LoadProfileFixture(t, apiconfig.APIWorkerIdentity(), "stack-worker-profile-api.json")
	worker := loaded.Profile.Workers[apiconfig.WorkerSourceObservation]

	worker.Executor.Enabled = false
	loaded.Profile.Workers = map[string]workercontract.WorkerProfile{apiconfig.WorkerSourceObservation: worker}

	registry := workercontract.NewRegistry(loaded, nil)
	require.NoError(t, registry.Register(runtime.WorkerRegistration()))
	require.NoError(t, registry.Seal())

	recorder := httptest.NewRecorder()
	registry.Handler().ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/diagnostics/workers", http.NoBody))
	require.Equal(t, http.StatusOK, recorder.Code)

	var diagnostics workercontract.DiagnosticsEnvelope

	require.NoError(t, jsonv2.Unmarshal(recorder.Body.Bytes(), &diagnostics))
	require.True(t, diagnostics.Complete)

	observation := diagnostics.Workers[apiconfig.WorkerSourceObservation]
	require.False(t, observation.Executor.Enabled)
	require.NotNil(t, observation.Queue.Depth)
	require.Equal(t, int64(2), *observation.Queue.Depth)
	require.Zero(t, runtime.workerTracker.Snapshot(time.Now()).RunningWorkers)
	require.Zero(t, runtime.workerTracker.Snapshot(time.Now()).InFlight)

	require.NoError(t, runtime.Shutdown(t.Context()))

	var pending int

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM source_observation_queue WHERE status = 'PENDING'").Scan(&pending))
	require.Equal(t, 2, pending, "disabled runtime must leave durable work unclaimed")
}

package collectorruntime

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

func TestCandidatePollingSkipsUntilHintOrTenSeconds(t *testing.T) {
	for _, tc := range []struct {
		name string
		hint time.Duration
		wait time.Duration
	}{
		{"NULL 힌트", 0, 10 * time.Second},
		{"먼 힌트 상한", time.Hour, 10 * time.Second},
		{"가까운 힌트", 3 * time.Second, 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newLifecycleScheduler(t)
				stub := newEmptyCandidateStub(t)

				s.candidates = stub

				for _, runner := range s.discoveryRunners() {
					page := joblease.CandidatePage{}

					if tc.hint != 0 {
						page.NextDueAt = time.Now().Add(tc.hint)
					}

					stub.pages[runner.Contract().ID().String()] = page
				}

				s.discoverOnce(t.Context())

				queries := stub.queries
				require.Equal(t, len(s.discoveryRunners()), queries, "기동 직후 모두 조회")
				time.Sleep(tc.wait - time.Nanosecond)
				s.discoverOnce(t.Context())
				require.Equal(t, queries, stub.queries)
				time.Sleep(time.Nanosecond)
				s.discoverOnce(t.Context())
				require.Equal(t, 2*queries, stub.queries, "경계 시각에는 다시 조회")
				require.Equal(t, 3, stub.generationCalls, "generation은 건너뛰지 않음")
			})
		})
	}
}

func TestCandidatePollingDoesNotSkipFullTruncatedOrFailedPages(t *testing.T) {
	t.Parallel()

	id := collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"}

	for _, tc := range []struct {
		name string
		page joblease.CandidatePage
		err  error
	}{
		{"limit만큼 후보", joblease.CandidatePage{Jobs: make([]joblease.JobSpec, 2)}, nil},
		{"truncated", joblease.CandidatePage{Truncated: true}, nil},
		{"조회 실패", joblease.CandidatePage{}, errors.New("조회 실패")},
		{"지난 힌트", joblease.CandidatePage{NextDueAt: time.Now().Add(-time.Second)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &leaseScheduler{}
			s.setProjection(1)

			now := time.Now()
			s.recordCandidatePoll(id, 1, 0, now, joblease.CandidatePage{}, 2, nil)
			s.recordCandidatePoll(id, 1, 0, now, tc.page, 2, tc.err)
			require.False(t, s.skipCandidatePoll(id, now))
		})
	}
}

func TestCandidatePollingGenerationAndRunnerInvalidation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newLifecycleScheduler(t)
		stub := newEmptyCandidateStub(t)

		s.candidates = stub
		s.discoverOnce(t.Context())

		queries := stub.queries
		runner := s.discoveryRunners()[0].Contract().ID()
		s.invalidateCandidatePoll(runner)
		s.discoverOnce(t.Context())
		require.Equal(t, queries+1, stub.queries, "해당 runner만 무효화")

		stub.generation = 2

		s.discoverOnce(t.Context())
		require.Equal(t, 2*queries+1, stub.queries, "새 generation은 전부 무효화")
	})
}

func TestCandidatePollingCompletionDuringQueryDoesNotRestoreHint(t *testing.T) {
	t.Parallel()

	s := &leaseScheduler{}
	s.setProjection(1)

	id := collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"}
	revision := s.candidatePollSnapshotRevision(id)
	s.invalidateCandidatePoll(id)
	s.recordCandidatePoll(id, 1, revision, time.Now(), joblease.CandidatePage{}, 2, nil)
	require.False(t, s.skipCandidatePoll(id, time.Now()))
	s.setProjection(2)
	s.recordCandidatePoll(id, 1, s.candidatePollSnapshotRevision(id), time.Now(), joblease.CandidatePage{}, 2, nil)
	require.False(t, s.skipCandidatePoll(id, time.Now()), "구 generation의 응답은 대기를 복구하지 않음")
}

func TestCandidatePollingCompletionAfterExclusionSnapshotDoesNotRestoreHint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newLifecycleScheduler(t)
		stub := newEmptyCandidateStub(t)

		s.candidates = stub
		s.discoverOnce(t.Context())

		_, excluded, _ := s.discoverySnapshot()
		runners := s.discoveryRunners()
		id := runners[0].Contract().ID()
		s.invalidateCandidatePoll(id)
		s.setProjection(2)

		_, err := s.queryRunnerPage(t.Context(), stub, 2, runners)(0, excluded, 2)
		require.NoError(t, err)
		require.False(t, s.skipCandidatePoll(id, time.Now()), "조회 전 종료·generation 교체도 무효화를 보존")
	})
}

func TestCandidatePollingSkippedRunnerPreservesCapacityRotation(t *testing.T) {
	t.Parallel()

	var queried []int

	outcome := runCapacityAwareCycle(&capacityCycleRequest{
		runnerCount: 4, remaining: 1, batch: 4,
		skip: func(runner int) bool { return runner == 0 || runner == 2 },
		query: func(runner int, _ []string, limit int) (joblease.CandidatePage, error) {
			queried = append(queried, runner)

			require.Equal(t, 1, limit)

			return joblease.CandidatePage{Jobs: []joblease.JobSpec{queueTestSpec("key")}}, nil
		},
		enqueue: func(*joblease.JobSpec) EnqueueResult { return EnqueueAccepted },
	})

	require.Equal(t, []int{1}, queried)
	require.Equal(t, 1, outcome.queried)
	require.Equal(t, 2, nextRotationCursor(0, 4, &outcome))
}

func TestCandidatePollingInvalidatesEveryJobExit(t *testing.T) {
	for _, name := range []string{"성공", "실패 DEFERRED", "superseded release", "acquire 실패"} {
		t.Run(name, func(t *testing.T) {
			var fatal []error

			runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

			if name == "실패 DEFERRED" {
				runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
					return collection.CollectResult{}, collecterr.New(contract.ErrorCollectionFailed, contract.ClassTransient, "수집 실패")
				}
			}

			if name == "superseded release" {
				runner.collect = func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
					return collection.CollectResult{}, collection.ErrTargetDisabled
				}
			}

			executor, spec := newExecutorFixture(t, runner, &fatal)

			if name == "acquire 실패" {
				_, err := executor.repository.Acquire(t.Context(), spec, "other-collector")
				require.NoError(t, err)
			}

			s := newScheduler(executor, newEmptyCandidateStub(t), &executor.collector)
			s.setProjection(1)

			id := runner.JobID()
			s.recordCandidatePoll(id, 1, 0, time.Now(), joblease.CandidatePage{}, 2, nil)
			require.True(t, s.skipCandidatePoll(id, time.Now()))

			job, ok := executor.admit(spec, time.Now(), executor.collector.QueueMaxAge)
			require.True(t, ok)
			s.runQueued(t.Context(), &job)
			require.False(t, s.skipCandidatePoll(id, time.Now()))

			var state string

			require.NoError(t, executor.publisher.contracts.pool.QueryRow(t.Context(),
				`SELECT slot_state FROM youtube_collection_job_leases WHERE job_key=$1`, spec.JobKey).Scan(&state))

			want := "IDLE"

			if name == "실패 DEFERRED" {
				want = payloadBoundaryDeferredState
			}

			if name == "acquire 실패" {
				want = "ACTIVE"
			}

			require.Equal(t, want, state)
		})
	}
}

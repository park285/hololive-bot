package collectorruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

const (
	healthyRunnerID = "healthy"
	brokenRunnerID  = "broken"
)

func localCandidateFailure() error {
	return fmt.Errorf("%w: %w", joblease.ErrCandidateContract,
		collecterr.New(collecterr.Internal, collecterr.ClassInternal, "target bundle has mixed poll intervals"))
}

func TestDiscoveryRepeatedLocalFailurePreservesHealthyProgress(t *testing.T) {
	t.Parallel()

	for _, failedIndex := range []int{0, 2, 4} {
		for _, capacity := range []int{1, 5} {
			t.Run(fmt.Sprintf("failed-%d/capacity-%d", failedIndex, capacity), func(t *testing.T) {
				t.Parallel()
				checkLocalFailureProgress(t, failedIndex, capacity)
			})
		}
	}
}

func checkLocalFailureProgress(t *testing.T, failedIndex, capacity int) {
	t.Helper()

	ids := fairnessRunnerIDs(5)
	seen := make(map[string]int)
	cursor := 0
	failures := 0

	for range 10 {
		outcome := runCapacityAwareCycle(&capacityCycleRequest{
			runnerCount: len(ids), start: cursor, remaining: capacity, batch: capacity,
			query: func(runner int, excluded []string, limit int) (joblease.CandidatePage, error) {
				if runner == failedIndex {
					failures++
					return joblease.CandidatePage{}, localCandidateFailure()
				}

				return fairnessCandidatePage(ids[runner], excluded, limit)
			},
			enqueue: func(spec *joblease.JobSpec) EnqueueResult {
				seen[spec.JobKey]++
				return EnqueueAccepted
			},
		})
		if outcome.globalFailure || outcome.canceled || outcome.enqueued > capacity {
			t.Fatalf("outcome = %+v", outcome)
		}

		if len(outcome.failures) > 0 && !errors.Is(outcome.queryErr, joblease.ErrCandidateContract) {
			t.Fatalf("local failure disappeared: %+v", outcome)
		}

		cursor = nextRotationCursor(cursor, len(ids), &outcome)
	}

	if failures < 2 {
		t.Fatalf("failed runner lost query opportunity: %d", failures)
	}

	for index, id := range ids {
		if index != failedIndex && seen[id+":0"] < 2 {
			t.Fatalf("healthy runner %s starved: %v", id, seen)
		}
	}
}

func TestDiscoveryGlobalFailureStopsAfterLocalFailure(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{errors.New("database unavailable"), collection.ErrProjectionStale, context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()

			queries := 0
			ids := []string{"local", "global", healthyRunnerID}
			outcome := runCapacityAwareCycle(&capacityCycleRequest{
				runnerCount: len(ids), remaining: 3, batch: 3,
				query: func(runner int, _ []string, _ int) (joblease.CandidatePage, error) {
					queries++

					if ids[runner] == "local" {
						return joblease.CandidatePage{}, localCandidateFailure()
					}

					return joblease.CandidatePage{}, cause
				},
				enqueue: func(*joblease.JobSpec) EnqueueResult {
					t.Fatal("global failure admitted a job")

					return EnqueueAccepted
				},
			})

			if queries != 2 || !outcome.globalFailure || !errors.Is(outcome.queryErr, cause) || len(outcome.failures) != 2 ||
				outcome.failures[0].runner != 0 || outcome.failures[1].runner != 1 {
				t.Fatalf("queries=%d outcome=%+v", queries, outcome)
			}

			if nextRotationCursor(0, 3, &outcome) != 0 {
				t.Fatal("global failure advanced cursor")
			}
		})
	}
}

func TestDiscoveryLocalFailureDedupAndQueueFullKeepBudget(t *testing.T) {
	t.Parallel()

	for _, result := range []EnqueueResult{EnqueueAccepted, EnqueueFull} {
		t.Run(string(result), func(t *testing.T) {
			t.Parallel()

			warned := 0
			ids := []string{brokenRunnerID, healthyRunnerID, "later"}
			outcome := runCapacityAwareCycle(&capacityCycleRequest{
				runnerCount: len(ids), remaining: 1, batch: 4,
				query: func(runner int, _ []string, limit int) (joblease.CandidatePage, error) {
					id := ids[runner]

					if limit != 1 {
						t.Fatalf("limit=%d, want 1", limit)
					}

					if id == brokenRunnerID {
						return joblease.CandidatePage{}, localCandidateFailure()
					}

					if id == healthyRunnerID {
						return joblease.CandidatePage{Jobs: []joblease.JobSpec{{JobKey: "duplicate"}}}, nil
					}

					return joblease.CandidatePage{Jobs: []joblease.JobSpec{{JobKey: "later"}}}, nil
				},
				enqueue: func(spec *joblease.JobSpec) EnqueueResult {
					if spec.JobKey == "duplicate" {
						return EnqueueDeduped
					}

					return result
				},
				warnFull: func() { warned++ },
			})

			if outcome.queried != 3 || outcome.deduped != 1 || !outcome.queueFull || outcome.globalFailure {
				t.Fatalf("outcome=%+v", outcome)
			}

			if result == EnqueueAccepted && (outcome.enqueued != 1 || warned != 0) {
				t.Fatalf("accepted outcome=%+v warned=%d", outcome, warned)
			}

			if result == EnqueueFull && (outcome.enqueued != 0 || warned != 1) {
				t.Fatalf("full outcome=%+v warned=%d", outcome, warned)
			}
		})
	}
}

type runnerFailureSource struct {
	*stubCandidateSource

	failedID string
	cause    error
	cancel   context.CancelFunc
}

func (s *runnerFailureSource) CandidatesForProjection(ctx context.Context, generation int64, job collection.JobContract, excluded []string, limit int) (joblease.CandidatePage, error) {
	if job.ID().String() == s.failedID {
		if s.cancel != nil {
			s.cancel()
		}

		return joblease.CandidatePage{}, s.cause
	}

	return s.stubCandidateSource.CandidatesForProjection(ctx, generation, job, excluded, limit)
}

func TestDiscoveryRecordsFailureWhileAdvancingCursor(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	runners := scheduler.discoveryRunners()

	if len(runners) == 0 {
		t.Fatal("runner registry is empty")
	}

	ids := jobIDStrings(runners)

	var logged bytes.Buffer

	scheduler.executor.logger = slog.New(slog.NewTextHandler(&logged, nil))

	source := &runnerFailureSource{stubCandidateSource: newEmptyCandidateStub(t), failedID: ids[0], cause: localCandidateFailure()}

	for _, id := range ids[1:] {
		source.pages[id] = joblease.CandidatePage{Jobs: []joblease.JobSpec{{JobKey: id}}}
	}

	scheduler.candidates = source
	scheduler.queueCapacity = len(ids)
	scheduler.queue = make(chan joblease.JobSpec, len(ids))
	scheduler.discoverOnce(t.Context())

	if scheduler.rotationCursor == 0 || scheduler.Snapshot().LastCycleOperationCode != collecterr.OperationCandidateLoadFailed {
		t.Fatalf("cursor=%d snapshot=%+v", scheduler.rotationCursor, scheduler.Snapshot())
	}

	if scheduler.Snapshot().Enqueued != len(ids)-1 || scheduler.Snapshot().LastCycleCompletedAt.IsZero() {
		t.Fatalf("progress not recorded: %+v", scheduler.Snapshot())
	}

	if !strings.Contains(logged.String(), "job_kind="+string(runners[0].Contract().ID().Kind)) ||
		!strings.Contains(logged.String(), "phase=candidate_load") ||
		!strings.Contains(logged.String(), "error_class="+string(collecterr.ClassInternal)) {
		t.Fatalf("runner failure diagnostic missing: %s", logged.String())
	}

	if _, invalid := scheduler.queued[ids[0]]; invalid {
		t.Fatal("invalid runner was admitted")
	}
}

func TestDiscoveryParentCancelWinsOverLocalError(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	ids := jobIDStrings(scheduler.discoveryRunners())
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	source := &runnerFailureSource{stubCandidateSource: newEmptyCandidateStub(t), failedID: ids[0], cause: localCandidateFailure(), cancel: cancel}

	scheduler.candidates = source
	scheduler.discoverOnce(ctx)

	if source.queries != 0 || scheduler.rotationCursor != 0 || scheduler.Snapshot().Enqueued != 0 {
		t.Fatalf("canceled cycle kept progressing: queries=%d cursor=%d snapshot=%+v", source.queries, scheduler.rotationCursor, scheduler.Snapshot())
	}
}

func TestDiscoveryLocalFailureCapacityOneStartsAfterLastQueriedRunner(t *testing.T) {
	t.Parallel()

	ids := []string{brokenRunnerID, healthyRunnerID, "later"}
	queried := []string{}
	outcome := runCapacityAwareCycle(&capacityCycleRequest{
		runnerCount: len(ids), remaining: 1, batch: 1,
		query: func(runner int, _ []string, _ int) (joblease.CandidatePage, error) {
			id := ids[runner]

			queried = append(queried, id)

			if id == brokenRunnerID {
				return joblease.CandidatePage{}, localCandidateFailure()
			}

			return joblease.CandidatePage{Jobs: []joblease.JobSpec{{JobKey: id}}}, nil
		},
		enqueue: func(*joblease.JobSpec) EnqueueResult { return EnqueueAccepted },
	})

	if !slices.Equal(queried, []string{brokenRunnerID, healthyRunnerID}) || nextRotationCursor(0, 3, &outcome) != 2 {
		t.Fatalf("queried=%v outcome=%+v", queried, outcome)
	}
}

func TestDiscoveryFullQueueDoesNotQuery(t *testing.T) {
	t.Parallel()

	scheduler := newLifecycleScheduler(t)
	source := newEmptyCandidateStub(t)

	scheduler.candidates = source
	scheduler.queueCapacity = 1
	scheduler.queued["pending"] = struct{}{}
	scheduler.discoverOnce(t.Context())

	if source.generationCalls != 0 || source.queries != 0 || scheduler.rotationCursor != 0 || !scheduler.Snapshot().QueueFull {
		t.Fatalf("full queue discovery: generation=%d queries=%d snapshot=%+v", source.generationCalls, source.queries, scheduler.Snapshot())
	}
}

func jobIDStrings(runners []RegisteredRunner) []string {
	ids := make([]string, len(runners))
	for i, runner := range runners {
		ids[i] = runner.Contract().ID().String()
	}

	return ids
}

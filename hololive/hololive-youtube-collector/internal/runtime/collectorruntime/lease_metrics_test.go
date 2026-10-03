package collectorruntime

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

func leaseMetricSpec() *joblease.JobSpec {
	return &joblease.JobSpec{JobKey: testJobKey, Provider: contract.ProviderYouTubeJS, CollectionJobKind: testCommunityJobKind, SubjectKey: testSubjectKey}
}

func leaseLostByPhase(t *testing.T, registerer prometheus.Gatherer, phase string) float64 {
	t.Helper()

	return metricValue(t, registerer, "youtube_collection_lease_lost_total", map[string]string{
		labelProvider: string(contract.ProviderYouTubeJS), labelKind: testCommunityJobKind, "phase": phase,
	})
}

func gaugeValue(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) (float64, bool) {
	t.Helper()

	families, err := registerer.Gather()
	if err != nil {
		t.Fatal(err)
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		for _, metric := range family.Metric {
			if metricLabelsMatch(metric.GetLabel(), labels) {
				return metric.GetGauge().GetValue(), true
			}
		}
	}

	return 0, false
}

// publish terminal에서 생긴 fence 손실은 callback join 뒤 phase=publish로 한 번만 센다(collect 중복 없음).
func TestPublishFenceLossIsCountedOnceAsPublish(t *testing.T) {
	t.Parallel()

	var fatal []error

	executor := newRunErrorExecutor(&fatal)
	registerer := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(registerer)

	spec := leaseMetricSpec()
	lease := &supersededLease{}
	publishErr := markLeasePhase(phasePublish, fmt.Errorf("publish complete: %w",
		collecterr.Wrap(collecterr.PublishRejected, collecterr.ClassTransient, sourceobservation.ErrCollectionFenceLost)))

	executor.handleRunError(t.Context(), lease, spec, &contract.LeaseProof{}, fmt.Errorf("run collection job: %w", publishErr))

	if got := leaseLostByPhase(t, registerer, phasePublish); got != 1 {
		t.Fatalf("publish lease lost = %v, want 1", got)
	}

	if got := leaseLostByPhase(t, registerer, phaseCollect); got != 0 {
		t.Fatalf("collect lease lost = %v, want 0", got)
	}

	if lease.releaseCall != 0 {
		t.Fatalf("release calls = %d, owner loss must not release", lease.releaseCall)
	}
}

// renew가 확인한 실제 소유 손실은 phase=renew다.
func TestRenewFenceLossIsCountedAsRenew(t *testing.T) {
	t.Parallel()

	var fatal []error

	executor := newRunErrorExecutor(&fatal)
	registerer := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(registerer)

	result := joblease.LeaseRunResult{Outcome: joblease.LeaseRunFenceLost, Err: errors.Join(joblease.ErrFenceLost, nil)}
	if !executor.handleLeaseRunOutcome(t.Context(), result, leaseMetricSpec(), &contract.LeaseProof{}) {
		t.Fatal("renew fence loss was not handled")
	}

	if got := leaseLostByPhase(t, registerer, phaseRenew); got != 1 {
		t.Fatalf("renew lease lost = %v, want 1", got)
	}

	if got := leaseLostByPhase(t, registerer, phaseCollect); got != 0 {
		t.Fatalf("collect lease lost = %v, want 0", got)
	}
}

// membership 무효로 join 뒤 superseded release까지 끝난 renew는 lease 손실이 아니며 executor가 다시 해제하지 않는다.
func TestRenewSupersededIsHandledWithoutLeaseLossOrSecondRelease(t *testing.T) {
	t.Parallel()

	var fatal []error

	executor := newRunErrorExecutor(&fatal)
	registerer := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(registerer)

	err := fmt.Errorf("run collection job: superseded renew: %w", errors.Join(joblease.ErrTargetDisabled, nil))
	result := joblease.LeaseRunResult{Outcome: joblease.LeaseRunReleasedAfterSuperseded, Err: err}

	if !executor.handleLeaseRunOutcome(t.Context(), result, leaseMetricSpec(), &contract.LeaseProof{}) {
		t.Fatal("superseded renew must not fall through to handleRunError")
	}

	for _, phase := range []string{phaseRenew, phaseCollect, phasePublish} {
		if got := leaseLostByPhase(t, registerer, phase); got != 0 {
			t.Fatalf("%s lease lost = %v, want 0", phase, got)
		}
	}

	if attemptResult(err) != resultSuperseded || len(fatal) != 0 {
		t.Fatalf("attempt result = %q fatal = %v", attemptResult(err), fatal)
	}
}

// 획득 시점의 membership·projection 무효는 acquire 오류가 아니라 superseded로 센다.
func TestAcquireSupersededIsLabeled(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{joblease.ErrProjectionStale, joblease.ErrTargetDisabled} {
		registerer := prometheus.NewPedanticRegistry()
		executor := &collectionExecutor{metrics: NewMetrics(registerer)}

		executor.observeAcquireError(t.Context(), leaseMetricSpec(), fmt.Errorf("acquire: %w", cause))

		labels := map[string]string{labelProvider: string(contract.ProviderYouTubeJS), labelKind: testCommunityJobKind}

		labels["result"] = resultSuperseded
		if got := metricValue(t, registerer, "youtube_collection_lease_acquire_total", labels); got != 1 {
			t.Fatalf("%v superseded acquire = %v, want 1", cause, got)
		}

		labels["result"] = resultError
		if got := metricValue(t, registerer, "youtube_collection_lease_acquire_total", labels); got != 0 {
			t.Fatalf("%v acquire error = %v, want 0", cause, got)
		}
	}
}

// Commit된 inserted·duplicate만 수락으로 기록하고, checkpoint가 전진한 관측만 간격을 남긴다. Collision은 수락이 아니다.
func TestObservePublishedRecordsDurableAcceptanceOnly(t *testing.T) {
	t.Parallel()

	observations := []contract.Envelope{
		{ObservationKind: contract.KindVideoList, Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous},
		{ObservationKind: contract.KindShortsList, Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous},
		{ObservationKind: contract.KindCommunityPage, Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous},
	}
	for i := range observations {
		observations[i].Provider = contract.ProviderYouTubeJS
		observations[i].Payload = []byte(`{"value":1}`)
	}

	output := testRunOutput(t, observations)
	registerer := prometheus.NewPedanticRegistry()
	executor := &collectionExecutor{metrics: NewMetrics(registerer)}
	before := time.Now().Unix()

	executor.observePublished(output, sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		{ObservationID: 1, Outcome: sourceobservation.PublishInserted, Ordinal: 0, AcceptedInterval: 5 * time.Minute, HasAcceptedInterval: true},
		{ObservationID: 2, Outcome: sourceobservation.PublishDuplicate, Ordinal: 1},
		{ObservationID: 3, Outcome: sourceobservation.PublishCollision, Ordinal: 2},
	}})

	for i, want := range []struct {
		accepted  bool
		intervals uint64
	}{{true, 1}, {true, 0}, {false, 0}} {
		metadata := output.ObservationMetadata(i)
		labels := map[string]string{labelProvider: string(metadata.Provider), labelKind: string(metadata.ObservationKind)}

		value, found := gaugeValue(t, registerer, "youtube_observation_last_accepted_timestamp_seconds", labels)
		if found != want.accepted || (found && value < float64(before)) {
			t.Fatalf("%s last accepted = %v found=%t, want accepted=%t", metadata.ObservationKind, value, found, want.accepted)
		}

		if got := histogramCount(t, registerer, "youtube_observation_accept_interval_seconds", labels); got != want.intervals {
			t.Fatalf("%s accept intervals = %d, want %d", metadata.ObservationKind, got, want.intervals)
		}
	}
}

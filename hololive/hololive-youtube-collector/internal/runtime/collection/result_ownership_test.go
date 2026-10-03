package collection

import (
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const testChangedSubject = "changed"

func TestCollectResultKeepsOwnedSnapshots(t *testing.T) {
	t.Parallel()

	for _, kind := range []CollectResultKind{CollectComplete, CollectPartial} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			checkResultOwnership(t, kind)
		})
	}
}

func checkResultOwnership(t *testing.T, kind CollectResultKind) {
	t.Helper()

	eventAt := time.Date(2026, time.October, 2, 1, 0, 0, 0, time.UTC)
	want := contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindVideoList,
		SubjectKey: "UC_TEST", ObservationKey: "observation", ContractGeneration: 1,
		ScheduledFor: eventAt, ObservedAt: eventAt, SourceEventAt: new(eventAt),
		ScopeSHA256: "scope", Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
		Payload: []byte(`{"value":1}`), EvidenceSHA256: "evidence", CollectorInstance: "collector",
		Lease: contract.LeaseProof{JobKey: "job", OwnerInstance: "collector", FenceEpoch: 1, ScheduledFor: eventAt},
	}
	observations := cloneEnvelopes([]contract.Envelope{want})

	output, err := NewRunOutput(observations, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	cause := errors.New("upstream failed")
	failed := []contract.ObservationKind{contract.KindShortsList}
	result := NewCompleteResult(output)

	if kind == CollectPartial {
		result, err = NewPartialResult(output, collecterr.Wrap(collecterr.Failed, collecterr.ClassTransient, cause), failed...)
	}

	if err != nil {
		t.Fatal(err)
	}

	observations[0].Payload[0] = 'x'
	*observations[0].SourceEventAt = eventAt.Add(time.Hour)
	observations[0].SubjectKey = testChangedSubject
	failed[0] = contract.KindCommunityPage

	checkConcurrentResultSnapshots(t, &result, &want)

	if !reflect.DeepEqual(output.Observations(), []contract.Envelope{want}) || output.CollectionLatency() != time.Second {
		t.Fatal("shared RunOutput changed through a result getter")
	}

	if result.Kind() != kind {
		t.Fatalf("result kind = %q, want %q", result.Kind(), kind)
	}

	checkResultPartialFailure(t, &result, kind, cause)
}

func checkConcurrentResultSnapshots(t *testing.T, result *CollectResult, want *contract.Envelope) {
	t.Helper()

	var readers sync.WaitGroup

	for range 4 {
		readers.Go(func() {
			for range 10 {
				snapshot := result.Output()
				got := snapshot.Observations()

				if !reflect.DeepEqual(got, []contract.Envelope{*want}) || snapshot.CollectionLatency() != time.Second {
					t.Error("result changed after mutating an input or another getter snapshot")

					return
				}

				got[0].Payload[0] = 'y'
				*got[0].SourceEventAt = want.ScheduledFor.Add(2 * time.Hour)
				got[0].SubjectKey = "getter changed"
			}
		})
	}

	readers.Wait()
}

func checkResultPartialFailure(t *testing.T, result *CollectResult, kind CollectResultKind, cause error) {
	t.Helper()

	partial, ok := result.PartialFailure()

	if kind == CollectComplete {
		if ok {
			t.Fatal("complete result contains a partial failure")
		}

		return
	}

	if !ok || !errors.Is(partial.Cause(), cause) {
		t.Fatal("partial failure lost its cause")
	}

	failed := partial.FailedKinds()
	if len(failed) != 1 || failed[0] != contract.KindShortsList {
		t.Fatalf("partial failed kinds = %v, want shorts", failed)
	}

	failed[0] = contract.KindCommunityPage

	if !slices.Equal(partial.FailedKinds(), []contract.ObservationKind{contract.KindShortsList}) {
		t.Fatal("partial failure getter exposed its failed kinds")
	}
}

func TestOutputMetadataIsIndependentOfMutableSnapshots(t *testing.T) {
	t.Parallel()

	envelope := contract.Envelope{
		Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindVideoList, SubjectKey: "UC_TEST",
		Completeness: contract.CompletenessPartial, Continuity: contract.ContinuityNotApplicable,
		Payload: []byte(`{"value":1}`),
	}

	output, err := OutputFromEnvelopes([]contract.Envelope{envelope}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	metadata := output.ObservationMetadata(0)

	metadata.SubjectKey = testChangedSubject
	output.Observations()[0].ObservationKind = contract.KindShortsList

	if metadata.SubjectKey == output.ObservationMetadata(0).SubjectKey {
		t.Fatal("metadata mutation changed the stored value")
	}

	if output.ObservationCount() != 1 ||
		output.ObservationMetadata(0).SubjectKey != envelope.SubjectKey ||
		output.ObservationMetadata(0).ObservationKind != envelope.ObservationKind {
		t.Fatal("metadata changed through a caller-owned value")
	}
}

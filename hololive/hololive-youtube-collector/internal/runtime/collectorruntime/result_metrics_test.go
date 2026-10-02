package collectorruntime

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

func TestObservePublishedPreservesMetadataAndSkipsUnknownResults(t *testing.T) {
	t.Parallel()

	observations := []contract.Envelope{
		{ObservationKind: contract.KindVideoList, Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous},
		{ObservationKind: contract.KindShortsList, Completeness: contract.CompletenessPartial, Continuity: contract.ContinuityNotApplicable},
		{ObservationKind: contract.KindCommunityPage, Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous},
		{ObservationKind: contract.KindChannelProfile, Completeness: contract.CompletenessPartial, Continuity: contract.ContinuityNotApplicable},
		{ObservationKind: contract.KindChannelPhoto, Completeness: contract.CompletenessPartial, Continuity: contract.ContinuityNotApplicable},
	}
	for i := range observations {
		observations[i].Provider = contract.ProviderYouTubeJS
		observations[i].Payload = []byte(`{"value":1}`)
	}

	output := testRunOutput(t, observations)

	observations[0].ObservationKind = contract.KindLiveSnapshot

	registerer := prometheus.NewPedanticRegistry()
	executor := &collectionExecutor{metrics: NewMetrics(registerer)}
	executor.observePublished(output, sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		sourceobservation.NewPublishedObservation(1, sourceobservation.PublishInserted, 0),
		sourceobservation.NewPublishedObservation(2, sourceobservation.PublishDuplicate, 1),
		sourceobservation.NewPublishedObservation(3, sourceobservation.PublishCollision, 2),
		{ObservationID: 4, Outcome: "UNKNOWN", Ordinal: 3},
	}})

	for i, outcome := range []string{outcomeInserted, outcomeDuplicate, outcomeCollision, outcomeRejected, outcomeRejected} {
		metadata := output.ObservationMetadata(i)
		want := float64(1)

		if i >= 3 {
			want = 0
		}

		labels := map[string]string{labelProvider: string(metadata.Provider), labelKind: string(metadata.ObservationKind), "outcome": outcome}
		if got := metricValue(t, registerer, "youtube_observation_publish_total", labels); got != want {
			t.Fatalf("publish metric for %s = %v, want %v", metadata.ObservationKind, got, want)
		}

		delete(labels, "outcome")

		labels["completeness"] = string(metadata.Completeness)
		labels["continuity"] = string(metadata.Continuity)

		if got := metricValue(t, registerer, "youtube_collection_completeness_total", labels); got != want {
			t.Fatalf("completeness metric for %s = %v, want %v", metadata.ObservationKind, got, want)
		}
	}
}

package collectorruntime

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestMetricsUseBoundedLabelVocabulary(t *testing.T) {
	t.Parallel()
	registerer := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registerer)
	metrics.ObserveAttempt(contract.ProviderYouTubeJS, "community_collect", "not-a-result", time.Second)
	metrics.ObserveAcquire(contract.ProviderYouTubeJS, "community_collect", "weird")
	metrics.ObserveLeaseLost(contract.ProviderYouTubeJS, "community_collect", "mystery")
	metrics.ObservePublish(contract.ProviderYouTubeJS, "community_page", "nope")
	metrics.ObserveCompleteness(contract.ProviderYouTubeJS, "community_page", contract.CompletenessComplete, contract.ContinuityContiguous)

	families, err := registerer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string][]string{}
	for _, family := range families {
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				seen[label.GetName()] = append(seen[label.GetName()], label.GetValue())
				if label.GetName() == "subject_key" || label.GetName() == "job_key" || strings.Contains(label.GetValue(), " ") {
					t.Fatalf("unbounded label %s=%q", label.GetName(), label.GetValue())
				}
			}
			_ = metric
		}
	}
	assertLabel(t, seen, "result", resultFailed)
	assertLabel(t, seen, "result", resultError)
	assertLabel(t, seen, "phase", phaseCollect)
	assertLabel(t, seen, "outcome", outcomeRejected)
}

func TestMetricsFreshnessAdvancesAfterSuccess(t *testing.T) {
	t.Parallel()
	registerer := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registerer)
	started := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	metrics.ObserveSuccess(contract.ProviderYouTubeJS, "community_collect", started)
	metrics.ObserveFreshness(contract.ProviderYouTubeJS, "community_collect", started.Add(5*time.Second))
	families, err := registerer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != "youtube_collection_freshness_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			if metric.GetGauge().GetValue() != 5 {
				t.Fatalf("freshness = %v, want 5", metric.GetGauge().GetValue())
			}
			return
		}
	}
	t.Fatal("freshness gauge was not recorded")
}

func TestMetricsTracksPublishedObservationForHandoffReadiness(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics(prometheus.NewRegistry())
	if _, _, ok := metrics.PublishedHandoff(); ok {
		t.Fatal("fresh metrics unexpectedly has a published observation")
	}
	metrics.ObservePublishedObservation(40)
	metrics.ObservePublishedObservation(41)
	if observationID, complete, ok := metrics.PublishedHandoff(); !ok || complete || observationID != 40 {
		t.Fatalf("PublishedHandoff() = %d, %v, %v", observationID, complete, ok)
	}
	metrics.ObserveHandoffComplete(40)
	if _, complete, _ := metrics.PublishedHandoff(); !complete {
		t.Fatal("published handoff did not become complete")
	}
}

func assertLabel(t *testing.T, seen map[string][]string, name, want string) {
	t.Helper()
	if slices.Contains(seen[name], want) {
		return
	}
	t.Fatalf("label %s missing %s in %v", name, want, seen[name])
}

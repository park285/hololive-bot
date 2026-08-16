package collectorruntime

import (
	"errors"
	"fmt"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestExpectedProjectionChurnUsesSupersededAttemptAndPublishMetrics(t *testing.T) {
	t.Parallel()
	for name, sourceErr := range map[string]error{
		"projection stale": sourceobservation.ErrProjectionStale,
		"target disabled":  sourceobservation.ErrTargetDisabled,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := collecterr.Wrap(collecterr.PublishRejected, fmt.Errorf("publish fence: %w", sourceErr))
			if got := attemptResult(err); got != resultSuperseded {
				t.Fatalf("attemptResult() = %q, want %q", got, resultSuperseded)
			}
			if !ignoreRunError(err) {
				t.Fatal("expected projection churn to remain ignored by run completion")
			}

			registerer := prometheus.NewPedanticRegistry()
			metrics := NewMetrics(registerer)
			metrics.ObserveAttempt(contract.ProviderYouTubeJS, "community_collect", attemptResult(err), time.Second)
			scheduler := &leaseScheduler{metrics: metrics}
			scheduler.observePublishError(
				&joblease.JobSpec{Provider: contract.ProviderYouTubeJS, CollectionJobKind: "community_collect"},
				collectutil.RunOutput{Observations: []contract.Envelope{{
					Provider:        contract.ProviderYouTubeJS,
					ObservationKind: contract.KindCommunityPage,
				}}},
				err,
			)

			if got := metricValue(t, registerer, "youtube_collection_attempts_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect", "result": resultSuperseded,
			}); got != 1 {
				t.Fatalf("superseded attempt count = %v, want 1", got)
			}
			if got := histogramCount(t, registerer, "youtube_collection_duration_seconds", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect",
			}); got != 1 {
				t.Fatalf("attempt duration count = %d, want 1", got)
			}
			if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeSuperseded,
			}); got != 1 {
				t.Fatalf("superseded publish count = %v, want 1", got)
			}
			if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
				"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeRejected,
			}); got != 0 {
				t.Fatalf("rejected publish count = %v, want 0", got)
			}
		})
	}
}

func TestUnknownPublishErrorRemainsFailedAndRejected(t *testing.T) {
	t.Parallel()
	err := collecterr.Wrap(collecterr.PublishRejected, errors.New("database unavailable"))
	if got := attemptResult(err); got != resultFailed {
		t.Fatalf("attemptResult() = %q, want %q", got, resultFailed)
	}
	if ignoreRunError(err) {
		t.Fatal("unexpected publish error was ignored")
	}

	registerer := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registerer)
	metrics.ObserveAttempt(contract.ProviderYouTubeJS, "community_collect", attemptResult(err), time.Second)
	scheduler := &leaseScheduler{metrics: metrics}
	scheduler.observePublishError(
		&joblease.JobSpec{Provider: contract.ProviderYouTubeJS, CollectionJobKind: "community_collect"},
		collectutil.RunOutput{Observations: []contract.Envelope{{
			Provider:        contract.ProviderYouTubeJS,
			ObservationKind: contract.KindCommunityPage,
		}}},
		err,
	)

	if got := metricValue(t, registerer, "youtube_collection_attempts_total", map[string]string{
		"provider": string(contract.ProviderYouTubeJS), "kind": "community_collect", "result": resultFailed,
	}); got != 1 {
		t.Fatalf("failed attempt count = %v, want 1", got)
	}
	if got := metricValue(t, registerer, "youtube_observation_publish_total", map[string]string{
		"provider": string(contract.ProviderYouTubeJS), "kind": string(contract.KindCommunityPage), "outcome": outcomeRejected,
	}); got != 1 {
		t.Fatalf("rejected publish count = %v, want 1", got)
	}
}

func metricValue(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) float64 {
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
			if !metricLabelsMatch(metric.GetLabel(), labels) {
				continue
			}
			return metric.GetCounter().GetValue()
		}
	}
	return 0
}

func histogramCount(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) uint64 {
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
			if !metricLabelsMatch(metric.GetLabel(), labels) {
				continue
			}
			return metric.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func metricLabelsMatch(labels []*dto.LabelPair, want map[string]string) bool {
	if len(labels) != len(want) {
		return false
	}
	for _, label := range labels {
		if want[label.GetName()] != label.GetValue() {
			return false
		}
	}
	return true
}

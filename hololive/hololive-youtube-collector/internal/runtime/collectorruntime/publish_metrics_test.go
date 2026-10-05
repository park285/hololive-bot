package collectorruntime

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// 성공한 발행은 job kind별 발행 트랜잭션 소요 시간과 SQL에 보낸 관측 JSON 크기를 한 번씩 남긴다.
func TestCollectAndPublishRecordsPublishDurationAndEncodedBytes(t *testing.T) {
	var fatal []error

	payload := testCommunityPayload(t)
	runner := stubJob(contract.ProviderYouTubeJS, testCommunityJobKind, contract.KindCommunityPage)

	runner.collect = communityResultCollector(payload)

	executor, spec := newExecutorFixture(t, runner, &fatal)
	registry := prometheus.NewPedanticRegistry()

	executor.metrics = NewMetrics(registry)

	lease, err := executor.acquireLease(t.Context(), spec)
	if err != nil || lease == nil {
		t.Fatalf("acquire fixture lease: %v", err)
	}

	registration, _ := executor.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	proof := lease.Proof()

	if _, err := executor.collectAndPublish(t.Context(), registration, spec, lease, &proof); err != nil {
		t.Fatalf("collect and publish: %v", err)
	}

	labels := map[string]string{labelProvider: string(spec.Provider), labelKind: spec.CollectionJobKind}
	if got := histogramCount(t, registry, "youtube_observation_publish_duration_seconds", labels); got != 1 {
		t.Fatalf("publish duration samples = %d, want 1", got)
	}

	if got := histogramCount(t, registry, "youtube_observation_publish_encoded_bytes", labels); got != 1 {
		t.Fatalf("publish encoded bytes samples = %d, want 1", got)
	}

	// 인코딩한 배치는 payload 외에 식별자·hash·lease 필드를 싣는다.
	if got := histogramSum(t, registry, "youtube_observation_publish_encoded_bytes", labels); got <= float64(len(payload)) {
		t.Fatalf("publish encoded bytes = %v, want more than payload %d", got, len(payload))
	}
}

func testCommunityPayload(t *testing.T) []byte {
	t.Helper()

	payload, err := contract.MarshalPayloadV1(contract.CommunityPayloadV1{
		ChannelID: testSubjectKey,
		Posts:     []contract.CommunityPostV1{{PostID: "post-metrics", ChannelID: testSubjectKey}},
		Coverage:  contract.CommunityPageCoverageV1{ChannelID: testSubjectKey, MaxResults: 10, PageCount: 1, Exhausted: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	return payload
}

// communityResultCollector는 실행 입력의 lease와 contract generation으로 community 관측 하나를 만든다.
func communityResultCollector(payload []byte) func(context.Context, *collection.RunInput) (collection.CollectResult, error) {
	return func(_ context.Context, input *collection.RunInput) (collection.CollectResult, error) {
		generation, err := input.Generation(contract.KindCommunityPage)
		if err != nil {
			return collection.CollectResult{}, fmt.Errorf("community generation: %w", err)
		}

		lease := input.Lease()

		envelope, err := contract.PrepareEnvelope(contract.Envelope{
			Provider: contract.ProviderYouTubeJS, ObservationKind: contract.KindCommunityPage, SubjectKey: input.Subject(),
			SchemaVersion: contract.SchemaVersionV1, ContractGeneration: generation,
			ScheduledFor: lease.ScheduledFor, ObservedAt: lease.ScheduledFor.Add(time.Second),
			Completeness: contract.CompletenessComplete, Continuity: contract.ContinuityContiguous,
			Payload: payload, CollectorInstance: lease.OwnerInstance, Lease: lease,
		})
		if err != nil {
			return collection.CollectResult{}, fmt.Errorf("prepare community envelope: %w", err)
		}

		output, err := collection.NewRunOutput([]contract.Envelope{envelope}, time.Second)
		if err != nil {
			return collection.CollectResult{}, fmt.Errorf("community run output: %w", err)
		}

		return collection.NewCompleteResult(output), nil
	}
}

func histogramSum(t *testing.T, registerer prometheus.Gatherer, name string, labels map[string]string) float64 {
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
				return metric.GetHistogram().GetSampleSum()
			}
		}
	}

	return 0
}

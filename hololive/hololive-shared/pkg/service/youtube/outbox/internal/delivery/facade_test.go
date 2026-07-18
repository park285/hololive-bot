package delivery_test

import (
	"errors"
	"reflect"
	"testing"

	delivery "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch"
)

func TestFacadeFunctionsDelegateToDispatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facade   any
		internal any
	}{
		{name: "NewDeliveryTelemetryRepository", facade: delivery.NewDeliveryTelemetryRepository, internal: dispatch.NewDeliveryTelemetryRepository},
		{name: "BuildChannelPostDeliverySummaries", facade: delivery.BuildChannelPostDeliverySummaries, internal: dispatch.BuildChannelPostDeliverySummaries},
		{name: "FormatYouTubeOutboxPayload", facade: delivery.FormatYouTubeOutboxPayload, internal: dispatch.FormatYouTubeOutboxPayload},
		{name: "DefaultConfig", facade: delivery.DefaultConfig, internal: dispatch.DefaultConfig},
		{name: "NewDispatcher", facade: delivery.NewDispatcher, internal: dispatch.NewDispatcher},
		{name: "BuildPostLatencyClassification", facade: delivery.BuildPostLatencyClassification, internal: dispatch.BuildPostLatencyClassification},
		{name: "BuildPostLatencyPeriodSummaries", facade: delivery.BuildPostLatencyPeriodSummaries, internal: dispatch.BuildPostLatencyPeriodSummaries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reflect.ValueOf(tc.facade).Pointer() != reflect.ValueOf(tc.internal).Pointer() {
				t.Fatalf("delivery.%s does not delegate to the dispatch implementation", tc.name)
			}
		})
	}
}

func TestErrDeliveryDedupeKeyRequiredMatchesDispatchSentinel(t *testing.T) {
	if !errors.Is(delivery.ErrDeliveryDedupeKeyRequired, dispatch.ErrDeliveryDedupeKeyRequired) {
		t.Fatal("delivery.ErrDeliveryDedupeKeyRequired is not the dispatch sentinel")
	}
	if !errors.Is(dispatch.ErrDeliveryDedupeKeyRequired, delivery.ErrDeliveryDedupeKeyRequired) {
		t.Fatal("dispatch.ErrDeliveryDedupeKeyRequired is not the delivery facade sentinel")
	}
}

func TestDefaultConfigMatchesDispatchDefaults(t *testing.T) {
	if got, want := delivery.DefaultConfig(), dispatch.DefaultConfig(); got != want {
		t.Fatalf("delivery.DefaultConfig() = %+v, want dispatch defaults %+v", got, want)
	}
}

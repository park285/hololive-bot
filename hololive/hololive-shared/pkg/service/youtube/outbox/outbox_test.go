package outbox_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	delivery "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

func TestFacadeFunctionsDelegateToInternalDelivery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		facade   any
		internal any
	}{
		{name: "NewDeliveryTelemetryRepository", facade: outbox.NewDeliveryTelemetryRepository, internal: delivery.NewDeliveryTelemetryRepository},
		{name: "BuildChannelPostDeliverySummaries", facade: outbox.BuildChannelPostDeliverySummaries, internal: delivery.BuildChannelPostDeliverySummaries},
		{name: "FormatYouTubeOutboxPayload", facade: outbox.FormatYouTubeOutboxPayload, internal: delivery.FormatYouTubeOutboxPayload},
		{name: "NewDeliveryRepository", facade: outbox.NewDeliveryRepository, internal: store.NewDeliveryRepository},
		{name: "DefaultConfig", facade: outbox.DefaultConfig, internal: delivery.DefaultConfig},
		{name: "NewDispatcher", facade: outbox.NewDispatcher, internal: delivery.NewDispatcher},
		{name: "BuildPostLatencyClassification", facade: outbox.BuildPostLatencyClassification, internal: delivery.BuildPostLatencyClassification},
		{name: "BuildPostLatencyPeriodSummaries", facade: outbox.BuildPostLatencyPeriodSummaries, internal: delivery.BuildPostLatencyPeriodSummaries},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reflect.ValueOf(tc.facade).Pointer() != reflect.ValueOf(tc.internal).Pointer() {
				t.Fatalf("outbox.%s does not delegate to the internal delivery implementation", tc.name)
			}
		})
	}
}

func TestErrDeliveryDedupeKeyRequiredMatchesInternalSentinel(t *testing.T) {
	if !errors.Is(outbox.ErrDeliveryDedupeKeyRequired, delivery.ErrDeliveryDedupeKeyRequired) {
		t.Fatal("outbox.ErrDeliveryDedupeKeyRequired is not the internal delivery sentinel")
	}
	if got, want := outbox.ErrDeliveryDedupeKeyRequired.Error(), "delivery dedupe key is required"; got != want {
		t.Fatalf("sentinel message = %q, want %q", got, want)
	}
}

func TestDefaultConfigProvidesUsableDispatchDefaults(t *testing.T) {
	config := outbox.DefaultConfig()

	for name, value := range map[string]int{
		"BatchSize":                   config.BatchSize,
		"MaxRetries":                  config.MaxRetries,
		"DeliveryParallelism":         config.DeliveryParallelism,
		"SubscriberLookupParallelism": config.SubscriberLookupParallelism,
		"TelemetryBackfillBatch":      config.TelemetryBackfillBatch,
		"TelemetryFlushBatch":         config.TelemetryFlushBatch,
	} {
		if value <= 0 {
			t.Errorf("DefaultConfig().%s = %d, want positive", name, value)
		}
	}

	for name, value := range map[string]time.Duration{
		"LockTimeout":           config.LockTimeout,
		"PollInterval":          config.PollInterval,
		"RetryBackoff":          config.RetryBackoff,
		"CleanupAfter":          config.CleanupAfter,
		"ReviveInterval":        config.ReviveInterval,
		"ReviveFreshnessWindow": config.ReviveFreshnessWindow,
		"ClaimFreshnessWindow":  config.ClaimFreshnessWindow,
		"DeliverySendTimeout":   config.DeliverySendTimeout,
		"AggregateSyncInterval": config.AggregateSyncInterval,
		"TelemetryPollInterval": config.TelemetryPollInterval,
		"TelemetryRetryBackoff": config.TelemetryRetryBackoff,
		"TelemetryRetention":    config.TelemetryRetention,
	} {
		if value <= 0 {
			t.Errorf("DefaultConfig().%s = %v, want positive", name, value)
		}
	}

	if !config.CleanupEnabled {
		t.Error("DefaultConfig().CleanupEnabled = false, want true")
	}
	if !config.ReviveEnabled {
		t.Error("DefaultConfig().ReviveEnabled = false, want true")
	}
	if config.ClaimFreshnessWindow < config.ReviveFreshnessWindow+config.ReviveInterval {
		t.Errorf(
			"DefaultConfig().ClaimFreshnessWindow = %v, want at least ReviveFreshnessWindow+ReviveInterval = %v",
			config.ClaimFreshnessWindow, config.ReviveFreshnessWindow+config.ReviveInterval,
		)
	}
}

func TestBuildChannelPostDeliverySummariesEmptyInputReturnsEmptySlice(t *testing.T) {
	summaries, err := outbox.BuildChannelPostDeliverySummaries(nil)
	if err != nil {
		t.Fatalf("BuildChannelPostDeliverySummaries(nil) error = %v", err)
	}
	if summaries == nil {
		t.Fatal("BuildChannelPostDeliverySummaries(nil) = nil slice, want empty slice")
	}
	if len(summaries) != 0 {
		t.Fatalf("BuildChannelPostDeliverySummaries(nil) len = %d, want 0", len(summaries))
	}
}

func TestBuildChannelPostDeliverySummariesRejectsBlankChannelID(t *testing.T) {
	detectedAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	_, err := outbox.BuildChannelPostDeliverySummaries([]outbox.PostSendCount{
		{ChannelID: "   ", ContentID: "post-a", AlarmType: domain.AlarmTypeCommunity, DetectedAt: &detectedAt},
	})
	if err == nil {
		t.Fatal("expected error for blank channel id, got nil")
	}
	if !strings.Contains(err.Error(), "channel id is empty") {
		t.Fatalf("error = %q, want it to mention empty channel id", err)
	}
}

func TestBuildChannelPostDeliverySummariesAggregatesPerChannelLatestFirst(t *testing.T) {
	detectedA := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	detectedB := detectedA.Add(time.Hour)
	sentB := detectedB.Add(2 * time.Minute)

	summaries, err := outbox.BuildChannelPostDeliverySummaries([]outbox.PostSendCount{
		{ChannelID: "UC-a", ContentID: "post-a", AlarmType: domain.AlarmTypeCommunity, DetectedAt: &detectedA},
		{ChannelID: "UC-b", ContentID: "post-b", AlarmType: domain.AlarmTypeShorts, DetectedAt: &detectedB, AlarmSentAt: &sentB, SuccessSendCount: 3},
	})
	if err != nil {
		t.Fatalf("BuildChannelPostDeliverySummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries len = %d, want 2", len(summaries))
	}

	if summaries[0].ChannelID != "UC-b" || summaries[1].ChannelID != "UC-a" {
		t.Fatalf("summary order = [%s, %s], want latest observed channel first [UC-b, UC-a]", summaries[0].ChannelID, summaries[1].ChannelID)
	}

	latest := summaries[0]
	if latest.DetectedPostCount != 1 || latest.AlarmSentPostCount != 1 || latest.SuccessPostCount != 1 {
		t.Fatalf("UC-b counts = detected %d, sent %d, success %d, want 1/1/1", latest.DetectedPostCount, latest.AlarmSentPostCount, latest.SuccessPostCount)
	}
	if latest.ShortsDetectedPostCount != 1 {
		t.Fatalf("UC-b shorts detected = %d, want 1", latest.ShortsDetectedPostCount)
	}

	unsent := summaries[1]
	if unsent.DetectedPostCount != 1 || unsent.AlarmSentPostCount != 0 || unsent.DetectedUnsentPostCount != 1 {
		t.Fatalf("UC-a counts = detected %d, sent %d, unsent %d, want 1/0/1", unsent.DetectedPostCount, unsent.AlarmSentPostCount, unsent.DetectedUnsentPostCount)
	}
	if unsent.CommunityDetectedPostCount != 1 {
		t.Fatalf("UC-a community detected = %d, want 1", unsent.CommunityDetectedPostCount)
	}
}

func TestBuildPostLatencyClassificationWithoutRowReportsInsufficientEvidence(t *testing.T) {
	result := outbox.BuildPostLatencyClassification(nil)

	if result.Status != outbox.PostLatencyClassificationStatusInsufficientEvidence {
		t.Fatalf("Status = %q, want insufficient evidence", result.Status)
	}
	if got, want := string(result.Status), "insufficient_evidence"; got != want {
		t.Fatalf("Status wire value = %q, want %q", got, want)
	}
	if result.DelaySource != outbox.PostDelaySourceNone {
		t.Fatalf("DelaySource = %q, want none", result.DelaySource)
	}
	if result.InternalDelayCause != outbox.PostInternalDelayCauseNone {
		t.Fatalf("InternalDelayCause = %q, want none", result.InternalDelayCause)
	}
	if result.ThresholdMillis <= 0 {
		t.Fatalf("ThresholdMillis = %d, want positive", result.ThresholdMillis)
	}
}

func TestBuildPostLatencyPeriodSummariesWithoutPeriodsReturnsEmptySlice(t *testing.T) {
	summaries, err := outbox.BuildPostLatencyPeriodSummaries(nil, nil)
	if err != nil {
		t.Fatalf("BuildPostLatencyPeriodSummaries(nil, nil) error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries len = %d, want 0", len(summaries))
	}
}

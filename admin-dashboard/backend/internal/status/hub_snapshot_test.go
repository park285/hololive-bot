package status

import (
	"testing"
	"time"
)

func TestPublishIsolatesInputSubscribersAndHistory(t *testing.T) {
	hub := NewHub(nil)
	_, updatesA, unsubscribeA := hub.Subscribe()
	defer unsubscribeA()
	_, updatesB, unsubscribeB := hub.Subscribe()
	defer unsubscribeB()

	errorText := "upstream unavailable"
	input := SystemStats{
		ThreadCount: 5,
		ServiceRuntime: []ServiceRuntimeStats{{
			Name:       "hololive-api",
			Count:      7,
			MetricKind: RuntimeMetricGoroutine,
			Available:  false,
			Error:      &errorText,
		}},
	}
	hub.Publish(&input)

	input.ServiceRuntime[0].Count = 99
	*input.ServiceRuntime[0].Error = "input mutated"

	gotA := receiveSystemStats(t, updatesA)
	gotB := receiveSystemStats(t, updatesB)
	gotA.ServiceRuntime[0].Count = 88
	*gotA.ServiceRuntime[0].Error = "subscriber mutated"

	assertOriginalRuntimeSnapshot(t, &gotB)

	history, _, unsubscribeHistory := hub.Subscribe()
	unsubscribeHistory()
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1", len(history))
	}
	assertOriginalRuntimeSnapshot(t, &history[0])

	history[0].ServiceRuntime[0].Count = 77
	*history[0].ServiceRuntime[0].Error = "history mutated"

	historyAgain, _, unsubscribeAgain := hub.Subscribe()
	unsubscribeAgain()
	if len(historyAgain) != 1 {
		t.Fatalf("second history len = %d, want 1", len(historyAgain))
	}
	assertOriginalRuntimeSnapshot(t, &historyAgain[0])
}

func receiveSystemStats(t *testing.T, updates <-chan SystemStats) SystemStats {
	t.Helper()
	select {
	case stats := <-updates:
		return stats
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive published stats")
		return SystemStats{}
	}
}

func assertOriginalRuntimeSnapshot(t *testing.T, stats *SystemStats) {
	t.Helper()
	if len(stats.ServiceRuntime) != 1 {
		t.Fatalf("service runtime len = %d, want 1", len(stats.ServiceRuntime))
	}
	runtime := stats.ServiceRuntime[0]
	if runtime.Count != 7 {
		t.Fatalf("runtime count = %d, want 7", runtime.Count)
	}
	if runtime.Error == nil || *runtime.Error != "upstream unavailable" {
		t.Fatalf("runtime error = %v, want upstream unavailable", runtime.Error)
	}
}

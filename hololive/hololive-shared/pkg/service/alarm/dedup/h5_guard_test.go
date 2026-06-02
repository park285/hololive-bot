package dedup_test

import (
	"encoding/json"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
)

func TestH5_NotifiedDataWireRoundTrip(t *testing.T) {
	t.Parallel()

	orig := dedup.NotifiedData{
		StartScheduled: "x",
		SentAt:         map[int]bool{1: true},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"start_scheduled":"x","sent_at":{"1":true}}`
	if string(b) != want {
		t.Fatalf("wire = %q, want %q", string(b), want)
	}

	var got dedup.NotifiedData
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.StartScheduled != orig.StartScheduled {
		t.Fatalf("StartScheduled = %q, want %q", got.StartScheduled, orig.StartScheduled)
	}
	if !got.SentAt[1] {
		t.Fatalf("SentAt[1] = false, want true")
	}
}

func TestH5_UpcomingEventNotifiedDataWireRoundTrip(t *testing.T) {
	t.Parallel()

	orig := dedup.UpcomingEventNotifiedData{NotifiedAt: "2026-01-01T00:00:00Z"}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"notified_at":"2026-01-01T00:00:00Z"}`
	if string(b) != want {
		t.Fatalf("wire = %q, want %q", string(b), want)
	}
	var got dedup.UpcomingEventNotifiedData
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.NotifiedAt != orig.NotifiedAt {
		t.Fatalf("NotifiedAt = %q, want %q", got.NotifiedAt, orig.NotifiedAt)
	}
}

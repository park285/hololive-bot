package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func benchPreparedDedupeInput() preparedDedupeInput {
	startScheduled := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:    "1234567890123456789",
			AlarmType: domain.AlarmTypeLive,
			Channel:   &domain.Channel{ID: "UC1DCedRgGHBdm81E1llLhOQ"},
			Stream: &domain.Stream{
				ID:             "dQw4w9WgXcQ",
				ChannelID:      "UC1DCedRgGHBdm81E1llLhOQ",
				Title:          "【歌枠】こんやも うたう よ～！ SINGING STREAM",
				StartScheduled: &startScheduled,
			},
			MinutesUntil: 10,
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			Kind:      domain.OutboxKindNewVideo,
			AlarmType: domain.AlarmTypeLive,
			ChannelID: "UC1DCedRgGHBdm81E1llLhOQ",
			Items: []domain.YouTubeOutboxItem{{
				ContentID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				Payload:   `{}`,
			}},
		},
	}
	input := prepareEnvelopeDedupeInput(&envelope)
	input.input.SourceOutboxKind = ""
	return input
}

func TestBuildDedupeKeyAllocationBudget(t *testing.T) {
	input := benchPreparedDedupeInput()

	dedupeAllocs := testing.AllocsPerRun(100, func() {
		if key := buildDedupeKey(input.input.RoomID, input.eventKey()); key == "" {
			t.Fatal("BuildDedupeKey returned empty key")
		}
	})
	if dedupeAllocs > dedupeKeyAllocBudget {
		t.Errorf("BuildDedupeKey allocs/op = %.1f, want <= %d", dedupeAllocs, dedupeKeyAllocBudget)
	}

	eventAllocs := testing.AllocsPerRun(100, func() {
		if key := input.eventKey(); key == "" {
			t.Fatal("BuildEventKey returned empty key")
		}
	})
	if eventAllocs > eventKeyAllocBudget {
		t.Errorf("BuildEventKey allocs/op = %.1f, want <= %d", eventAllocs, eventKeyAllocBudget)
	}
}

func BenchmarkBuildDedupeKey(b *testing.B) {
	input := benchPreparedDedupeInput()
	b.ReportAllocs()
	for b.Loop() {
		if key := buildDedupeKey(input.input.RoomID, input.eventKey()); key == "" {
			b.Fatal("BuildDedupeKey returned empty key")
		}
	}
}

func BenchmarkBuildEventKey(b *testing.B) {
	input := benchPreparedDedupeInput()
	b.ReportAllocs()
	for b.Loop() {
		if key := input.eventKey(); key == "" {
			b.Fatal("BuildEventKey returned empty key")
		}
	}
}

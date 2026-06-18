package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func benchDedupeInput() DedupeInput {
	return DedupeInput{
		RoomID:         "1234567890123456789",
		ChannelID:      "UC1DCedRgGHBdm81E1llLhOQ",
		AlarmType:      domain.AlarmTypeLive,
		StreamID:       "dQw4w9WgXcQ",
		Title:          "【歌枠】こんやも うたう よ～！ SINGING STREAM",
		StartScheduled: time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC),
		MinutesUntil:   10,
		Category:       "live",
		SourceKind:     domain.AlarmDispatchSourceKindYouTubeOutbox,
		SourceIdentity: "youtube:dQw4w9WgXcQ",
	}
}

func TestBuildDedupeKeyAllocationBudget(t *testing.T) {
	input := benchDedupeInput()

	dedupeAllocs := testing.AllocsPerRun(100, func() {
		if key := BuildDedupeKey(&input); key == "" {
			t.Fatal("BuildDedupeKey returned empty key")
		}
	})
	if dedupeAllocs > dedupeKeyAllocBudget {
		t.Errorf("BuildDedupeKey allocs/op = %.1f, want <= %d", dedupeAllocs, dedupeKeyAllocBudget)
	}

	eventAllocs := testing.AllocsPerRun(100, func() {
		if key := BuildEventKey(&input); key == "" {
			t.Fatal("BuildEventKey returned empty key")
		}
	})
	if eventAllocs > eventKeyAllocBudget {
		t.Errorf("BuildEventKey allocs/op = %.1f, want <= %d", eventAllocs, eventKeyAllocBudget)
	}
}

func BenchmarkBuildDedupeKey(b *testing.B) {
	input := benchDedupeInput()
	b.ReportAllocs()
	for b.Loop() {
		if key := BuildDedupeKey(&input); key == "" {
			b.Fatal("BuildDedupeKey returned empty key")
		}
	}
}

func BenchmarkBuildEventKey(b *testing.B) {
	input := benchDedupeInput()
	b.ReportAllocs()
	for b.Loop() {
		if key := BuildEventKey(&input); key == "" {
			b.Fatal("BuildEventKey returned empty key")
		}
	}
}

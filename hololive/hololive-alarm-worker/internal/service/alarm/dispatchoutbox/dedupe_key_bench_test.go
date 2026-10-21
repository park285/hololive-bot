package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func benchPreparedDedupeInput() DedupeInput {
	startScheduled := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
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
		SourceKind: domain.AlarmDispatchSourceKindDeliveryDigest,
		DeliveryDigest: &domain.DeliveryDigestDispatchPayload{
			Kind:               domain.DeliveryKindMajorEventWeekly,
			PeriodKey:          "2026-W24",
			PreRenderedMessage: "【歌枠】こんやも うたう よ～！ SINGING STREAM",
		},
	}

	return EnvelopeDedupeInput(&envelope)
}

func BenchmarkBuildDedupeKey(b *testing.B) {
	input := benchPreparedDedupeInput()

	b.ReportAllocs()

	for b.Loop() {
		if key := BuildDedupeKey(&input); key == "" {
			b.Fatal("BuildDedupeKey returned empty key")
		}
	}
}

func BenchmarkBuildEventKey(b *testing.B) {
	input := benchPreparedDedupeInput()

	b.ReportAllocs()

	for b.Loop() {
		if key := BuildEventKey(&input); key == "" {
			b.Fatal("BuildEventKey returned empty key")
		}
	}
}

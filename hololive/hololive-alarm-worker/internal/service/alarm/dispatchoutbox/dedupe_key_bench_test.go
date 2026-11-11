package dispatchoutbox

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func benchPreparedDedupeInput() DedupeInput {
	envelope := AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{RoomID: "1234567890123456789", AlarmType: domain.AlarmTypeLive},
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		XSpace: &domain.XSpaceDispatchPayload{
			SpaceID: "1YqKDqWqdPLGV", CreatorID: "1234567890", ChannelID: "UC1DCedRgGHBdm81E1llLhOQ",
			Title: "【歌枠】こんやも うたう よ～！ SINGING STREAM", StartedAt: time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC),
		},
	}

	return EnvelopeDedupeInput(&envelope)
}

func BenchmarkDedupeKey(b *testing.B) {
	input := benchPreparedDedupeInput()

	b.ReportAllocs()

	for b.Loop() {
		if key := buildDedupeKey(input.RoomID, BuildEventKey(&input)); key == "" {
			b.Fatal("buildDedupeKey returned empty key")
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

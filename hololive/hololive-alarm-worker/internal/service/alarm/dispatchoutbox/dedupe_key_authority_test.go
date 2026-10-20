package dispatchoutbox

import (
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type authorityKeyCase struct {
	name      string
	input     DedupeInput
	wantEvent string
}

func TestBuildEventKeyAuthorityHeadGoldens(t *testing.T) {
	startScheduled := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	tests := append(overLengthAuthorityKeyCases(), fixedFormatAuthorityKeyCases(startScheduled)...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAuthorityKeyPair(t, &tt.input, tt.wantEvent)
		})
	}
}

func overLengthAuthorityKeyCases() []authorityKeyCase {
	return []authorityKeyCase{{
		name: "over length live key",
		input: DedupeInput{
			RoomID:       testRoomID,
			ChannelID:    strings.Repeat("c", 600),
			AlarmType:    domain.AlarmTypeLive,
			StreamID:     strings.Repeat("s", 600),
			Category:     testClaimEventCategory,
			MinutesUntil: 10,
		},
		wantEvent: "event_sha:b9d7a14ee0e3277ce8304b4e84a77440cde6d6dc5bbb97e8e4d9975677c1f5d8",
	}}
}

func fixedFormatAuthorityKeyCases(startScheduled time.Time) []authorityKeyCase {
	return []authorityKeyCase{
		{
			name: "live key",
			input: DedupeInput{
				RoomID:         testRoomID,
				ChannelID:      testChannelID,
				AlarmType:      domain.AlarmTypeLive,
				StreamID:       testStreamID,
				StartScheduled: startScheduled,
				Category:       "live",
			},
			wantEvent: "live:channel-1:stream-1:1781265600:live:LIVE",
		},
		{
			name: "schedule key",
			input: DedupeInput{
				RoomID:                      testRoomID,
				ChannelID:                   testChannelID,
				AlarmType:                   domain.AlarmTypeLive,
				StreamID:                    testStreamID,
				StartScheduled:              startScheduled,
				ScheduleChangePreviousStart: "2026-06-12T11:00:00Z",
				Category:                    "live",
			},
			wantEvent: "schedule:channel-1:stream-1:2026-06-12T11:00:00Z:1781265600:live:LIVE",
		},
		{
			name: "celebration key",
			input: DedupeInput{
				RoomID:         testRoomID,
				SourceKind:     domain.AlarmDispatchSourceKindCelebration,
				SourceIdentity: "birthday:UC_test:2026-05-26",
			},
			wantEvent: "celebration:birthday:UC_test:2026-05-26",
		},
	}
}

func assertAuthorityKeyPair(t *testing.T, input *DedupeInput, wantEvent string) {
	t.Helper()

	if got := BuildEventKey(input); got != wantEvent {
		t.Fatalf("BuildEventKey() = %q, want %q", got, wantEvent)
	}

	wantDedupe := "v2:room:" + input.RoomID + ":event:" + wantEvent
	if got := BuildDedupeKey(input); got != wantDedupe {
		t.Fatalf("BuildDedupeKey() = %q, want %q", got, wantDedupe)
	}
}

package youtubejscollector

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// helper가 재사용한 일정은 이번 lease 슬롯보다 앞선 슬롯의 관측이어야 발행한다.
func TestValidateLiveSchedulesBoundsReusedObservationTime(t *testing.T) {
	t.Parallel()

	slot := time.Date(2026, time.November, 6, 1, 0, 0, 0, time.UTC)
	scheduled := slot.Add(10 * time.Hour)
	session := func(observed time.Time) []youtubejs.LiveSessionItem {
		return []youtubejs.LiveSessionItem{{
			VideoID: "v1", ChannelID: "UC_TEST", Status: "UPCOMING",
			ScheduledAt: new(scheduled), ScheduleObservedAt: new(observed),
		}}
	}

	if err := validateLiveSchedules(session(slot.Add(-2*time.Minute)), slot); err != nil {
		t.Fatalf("earlier slot observation rejected: %v", err)
	}

	for name, observed := range map[string]time.Time{
		"same slot":           slot,
		"later slot":          slot.Add(time.Second),
		"beyond reuse window": slot.Add(-contract.LiveScheduleObservationMaxAge - time.Second),
	} {
		if err := validateLiveSchedules(session(observed), slot); collecterr.CodeOf(err) != contract.ErrorParserDrift {
			t.Fatalf("%s: error = %v, want parser drift", name, err)
		}
	}
}

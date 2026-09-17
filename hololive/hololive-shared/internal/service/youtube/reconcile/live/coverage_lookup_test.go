package live

import (
	"testing"
	"time"
)

func TestAbsenceCoverageCacheUsesSlotIdentity(t *testing.T) {
	first, second := &AbsenceSlot{}, &AbsenceSlot{}

	first.ScheduledFor = time.Unix(100, 0)
	second.ScheduledFor = first.ScheduledFor
	first.Coverage.RequestedChannelIDs = []string{"a"}
	second.Coverage.RequestedChannelIDs = []string{"b"}
	first.Coverage.Filters.Statuses = []string{testLiveStatus}
	second.Coverage.Filters.Statuses = []string{testLiveStatus}

	session := &reduceSession{}
	existing := &SessionState{ChannelID: "a", Status: StatusLive}

	if !session.absenceCovers(first, existing) || session.absenceCovers(second, existing) {
		t.Fatal("equal scheduled_for values must not merge different coverage scopes")
	}

	if session.absenceCoverageSlot != second || !session.absenceCovers(first, existing) {
		t.Fatal("only the most recently visited slot may be retained")
	}

	if !session.absenceCovers(first, existing) || session.absenceCoverageSlot != first {
		t.Fatal("repeated queries must reuse the current slot")
	}

	other := &reduceSession{}
	if !other.absenceCovers(second, &SessionState{ChannelID: "b", Status: StatusLive}) {
		t.Fatal("coverage cache must be local to one reduction")
	}

	if session.absenceCoverageSlot != first {
		t.Fatal("another reduction changed the cached slot")
	}
}

//go:build !race

package dispatchoutbox

import "testing"

const (
	dedupeKeyAllocBudget = 5
	eventKeyAllocBudget  = 2
)

func TestDedupeKeyAllocationBudget(t *testing.T) {
	input := benchPreparedDedupeInput()

	dedupeAllocs := testing.AllocsPerRun(100, func() {
		if key := buildDedupeKey(input.RoomID, BuildEventKey(&input)); key == "" {
			t.Fatal("buildDedupeKey returned empty key")
		}
	})
	if dedupeAllocs > dedupeKeyAllocBudget {
		t.Errorf("event+dedupe key allocs/op = %.1f, want <= %d", dedupeAllocs, dedupeKeyAllocBudget)
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

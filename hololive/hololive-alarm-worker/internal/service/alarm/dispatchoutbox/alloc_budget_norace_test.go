//go:build !race

package dispatchoutbox

import "testing"

// race 계측은 할당을 늘리므로 운영 빌드의 할당 예산은 race가 아닌 실행에서만 검사한다.
// 이 테스트는 scripts/ci/go-test-nonrace.sh가 race 빌드와 별도로 실행한다.
const (
	dedupeKeyAllocBudget = 5
	eventKeyAllocBudget  = 2
)

func TestBuildDedupeKeyAllocationBudget(t *testing.T) {
	input := benchPreparedDedupeInput()

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

//go:build !race

package dispatchoutbox

const (
	dedupeKeyAllocBudget = 5
	eventKeyAllocBudget  = 2
)

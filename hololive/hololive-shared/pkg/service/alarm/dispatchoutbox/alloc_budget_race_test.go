//go:build race

package dispatchoutbox

const (
	dedupeKeyAllocBudget = 7
	eventKeyAllocBudget  = 2
)

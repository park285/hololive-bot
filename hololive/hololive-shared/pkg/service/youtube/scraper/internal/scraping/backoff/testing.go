package backoff

import "time"

func (b *BackoffState) SetTransientCooldownForTest(t time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.transientCooldown = t
}

func (b *BackoffState) TransientErrors() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.transientErrors
}

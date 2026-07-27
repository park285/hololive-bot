package apiclient

import (
	"context"
	"fmt"
)

func (c *APIClient) acquireSemaphore(ctx context.Context) error {
	select {
	case c.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("semaphore acquisition canceled: %w", ctx.Err())
	}
}

func (c *APIClient) releaseSemaphore() {
	select {
	case <-c.semaphore:
	default:
	}
}

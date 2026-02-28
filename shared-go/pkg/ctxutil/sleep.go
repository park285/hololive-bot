// Package ctxutil provides context-aware utilities for Go concurrency patterns.
package ctxutil

import (
	"context"
	"time"
)

// SleepWithContext performs a context-aware sleep operation.
// It waits for the specified duration or until the context is canceled,
// whichever occurs first.
//
// Returns:
//   - true if the sleep completed normally (duration elapsed)
//   - false if the context was canceled before duration elapsed
//
// This function is safe for concurrent use and properly cleans up timer resources.
//
// Example usage:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
//	defer cancel()
//
//	if SleepWithContext(ctx, 5*time.Second) {
//	    fmt.Println("Sleep completed")
//	} else {
//	    fmt.Println("Context canceled")
//	}
func SleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

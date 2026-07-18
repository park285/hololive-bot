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

package ctxutil_test

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/ctxutil"
)

func TestSleepWithContextReturnsTrueOnExpiry(t *testing.T) {
	start := time.Now()
	if !ctxutil.SleepWithContext(context.Background(), 10*time.Millisecond) {
		t.Fatal("SleepWithContext() = false, want true")
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("SleepWithContext() returned after %v, want >= 10ms", elapsed)
	}
}

func TestSleepWithContextReturnsFalseImmediatelyWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if ctxutil.SleepWithContext(ctx, time.Hour) {
		t.Fatal("SleepWithContext() = true, want false")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("SleepWithContext() returned after %v, want immediate return", elapsed)
	}
}

func TestSleepWithContextReturnsFalseOnMidSleepCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if ctxutil.SleepWithContext(ctx, time.Hour) {
		t.Fatal("SleepWithContext() = true, want false")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("SleepWithContext() returned after %v, want cancellation-bounded return", elapsed)
	}
}

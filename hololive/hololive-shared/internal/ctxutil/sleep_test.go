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

	"github.com/kapu/hololive-shared/internal/ctxutil"
)

func TestSleepWithContext(t *testing.T) {
	tests := []struct {
		name           string
		setupContext   func() (context.Context, context.CancelFunc)
		sleepDuration  time.Duration
		expectedResult bool
		description    string
	}{
		{
			name: "sleep_completes_normally",
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 200*time.Millisecond)
			},
			sleepDuration:  50 * time.Millisecond,
			expectedResult: true,
			description:    "정상 대기 완료 - context timeout(200ms)보다 짧은 sleep(50ms)",
		},
		{
			name: "context_cancelled_before_sleep",
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 50*time.Millisecond)
			},
			sleepDuration:  200 * time.Millisecond,
			expectedResult: false,
			description:    "context 취소 - context timeout(50ms)이 sleep(200ms)보다 빨리 발생",
		},
		{
			name: "immediate_cancellation",
			setupContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // 즉시 취소
				return ctx, cancel
			},
			sleepDuration:  100 * time.Millisecond,
			expectedResult: false,
			description:    "즉시 취소 - sleep 시작 전 이미 취소된 context",
		},
		{
			name: "zero_duration_sleep",
			setupContext: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 100*time.Millisecond)
			},
			sleepDuration:  0,
			expectedResult: true,
			description:    "0초 sleep - 즉시 완료",
		},
		{
			name: "manual_cancellation_during_sleep",
			setupContext: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				// 30ms 후 cancel 호출
				go func() {
					time.Sleep(30 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
			sleepDuration:  100 * time.Millisecond,
			expectedResult: false,
			description:    "수동 취소 - sleep 중간(30ms)에 cancel() 호출",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.setupContext()
			defer cancel()

			start := time.Now()
			result := ctxutil.SleepWithContext(ctx, tt.sleepDuration)
			elapsed := time.Since(start)

			if result != tt.expectedResult {
				t.Errorf("SleepWithContext() = %v, want %v\n설명: %s", result, tt.expectedResult, tt.description)
			}

			// 시간 검증 (결과에 따라 예상 소요 시간 확인)
			if tt.expectedResult {
				// sleep 완료: sleepDuration만큼 경과해야 함
				if elapsed < tt.sleepDuration {
					t.Errorf("Sleep completed too early: elapsed=%v, expected>=%v", elapsed, tt.sleepDuration)
				}
			} else {
				// context 취소: sleepDuration보다 빨리 종료되어야 함
				if elapsed >= tt.sleepDuration {
					t.Errorf("Sleep did not respect context cancellation: elapsed=%v, sleepDuration=%v", elapsed, tt.sleepDuration)
				}
			}
		})
	}
}

func TestSleepWithContext_Concurrency(t *testing.T) {
	const numGoroutines = 100
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan bool, numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			duration := time.Duration(id%5+1) * 10 * time.Millisecond // 10~50ms
			result := ctxutil.SleepWithContext(ctx, duration)
			done <- result
		}(i)
	}

	completedCount := 0
	cancelledCount := 0

	for range numGoroutines {
		if <-done {
			completedCount++
		} else {
			cancelledCount++
		}
	}

	t.Logf("Concurrency test: %d completed, %d cancelled", completedCount, cancelledCount)

	if completedCount+cancelledCount != numGoroutines {
		t.Errorf("Expected %d total results, got %d", numGoroutines, completedCount+cancelledCount)
	}
}

func BenchmarkSleepWithContext(b *testing.B) {
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctxutil.SleepWithContext(ctx, 1*time.Nanosecond)
	}
}

func BenchmarkSleepWithContext_Cancelled(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctxutil.SleepWithContext(ctx, 1*time.Second)
	}
}

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

package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/outputguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

func TestMonthlyScheduler_CalculateNextRun(t *testing.T) {
	scheduler := NewMonthlyScheduler(nil, nil, nil, nil, nil)

	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 1st target same month",
			now:  time.Date(2026, 3, 1, 9, 0, 0, 0, model.KST),
			want: time.Date(2026, 3, 1, 10, 0, 0, 0, model.KST),
		},
		{
			name: "after 1st target next month",
			now:  time.Date(2026, 3, 1, 10, 30, 0, 0, model.KST),
			want: time.Date(2026, 4, 1, 10, 0, 0, 0, model.KST),
		},
		{
			name: "exact 1st 10:00 target next month",
			now:  time.Date(2026, 3, 1, 10, 0, 0, 0, model.KST),
			want: time.Date(2026, 4, 1, 10, 0, 0, 0, model.KST),
		},
		{
			name: "year end december to january",
			now:  time.Date(2026, 12, 2, 0, 0, 0, 0, model.KST),
			want: time.Date(2027, 1, 1, 10, 0, 0, 0, model.KST),
		},
		{
			name: "leap year february to march",
			now:  time.Date(2028, 2, 1, 11, 0, 0, 0, model.KST), // 2028 = 윤년
			want: time.Date(2028, 3, 1, 10, 0, 0, 0, model.KST),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduler.calculateNextRun(tt.now)
			if !got.Equal(tt.want) {
				t.Fatalf("calculateNextRun(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

func TestMonthlyScheduler_LifecycleNilGuards(t *testing.T) {
	var scheduler *MonthlyScheduler

	scheduler.SetClock(time.Now)
	scheduler.Start(context.Background())
	scheduler.Stop()
}

func TestMonthlyScheduler_LockHeldSkip(t *testing.T) {
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: "room-1"}}}
	locker := &mockNotificationLocker{acquireAcquired: false}
	outbox := newMockOutboxRepository()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, model.KST)

	scheduler := NewMonthlyScheduler(service, mockFormatter{}, locker, outbox, nil, WithMonthlyOutputGuard(outputguard.NewGuard()))
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendMonthlyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error when lock held, got %v", err)
	}
	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("expected no enqueued items, got %d", len(outbox.enqueuedItems))
	}
}

func TestMonthlyScheduler_PartialEnqueueNoError(t *testing.T) {
	service := &mockDigestService{
		rooms: []model.SubscribedRoom{
			{RoomID: "room-fail"},
			{RoomID: "room-ok"},
		},
	}
	locker := &mockNotificationLocker{acquireToken: "tok", acquireAcquired: true}
	outbox := newMockOutboxRepository()
	outbox.enqueueErr["room-fail"] = errors.New("db error")
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, model.KST)

	scheduler := NewMonthlyScheduler(service, mockFormatter{}, locker, outbox, nil, WithMonthlyOutputGuard(outputguard.NewGuard()))
	scheduler.SetClock(func() time.Time { return now })

	if err := scheduler.SendMonthlyDigest(context.Background()); err != nil {
		t.Fatalf("expected no error for partial failure, got %v", err)
	}
	if len(outbox.enqueuedItems) != 1 {
		t.Errorf("expected 1 enqueued item, got %d", len(outbox.enqueuedItems))
	}
}

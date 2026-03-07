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

package membernews

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServiceGenerateRoomDigest_GuardBranches(t *testing.T) {
	t.Parallel()

	t.Run("nil service", func(t *testing.T) {
		t.Parallel()

		var svc *Service
		digest, err := svc.GenerateRoomDigest(context.Background(), "room-1", PeriodWeekly)
		if digest != nil {
			t.Fatalf("digest = %#v, want nil", digest)
		}
		if err == nil || !strings.Contains(err.Error(), "membernews service is nil") {
			t.Fatalf("error = %v, want membernews service is nil", err)
		}
	})

	t.Run("nil repository", func(t *testing.T) {
		t.Parallel()

		svc := &Service{}
		digest, err := svc.GenerateRoomDigest(context.Background(), "room-1", PeriodWeekly)
		if digest != nil {
			t.Fatalf("digest = %#v, want nil", digest)
		}
		if err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
			t.Fatalf("error = %v, want membernews repository is nil", err)
		}
	})

	t.Run("room id required", func(t *testing.T) {
		t.Parallel()

		svc := &Service{repository: &Repository{}}
		digest, err := svc.GenerateRoomDigest(context.Background(), "   ", PeriodWeekly)
		if digest != nil {
			t.Fatalf("digest = %#v, want nil", digest)
		}
		if err == nil || !strings.Contains(err.Error(), "room id is required") {
			t.Fatalf("error = %v, want room id is required", err)
		}
	})
}

func TestServiceSetClock_NilInputIsNoop(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 3, 5, 9, 30, 0, 0, time.UTC)
	svc := &Service{
		now: func() time.Time {
			return fixed
		},
	}

	svc.SetClock(nil)
	if got := svc.now(); !got.Equal(fixed) {
		t.Fatalf("SetClock(nil) changed clock: got %v, want %v", got, fixed)
	}

	var nilSvc *Service
	nilSvc.SetClock(nil)
}

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

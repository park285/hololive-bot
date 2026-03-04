package membernews

import (
	"context"
	"strings"
	"testing"
)

func TestNewService_SetsDefaultDependencies(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil)
	if svc == nil {
		t.Fatalf("NewService() returned nil")
	}
	if svc.logger == nil {
		t.Fatalf("NewService() logger is nil")
	}
	if svc.now == nil {
		t.Fatalf("NewService() now clock is nil")
	}
}

func TestService_SubscriptionMethodGuards(t *testing.T) {
	ctx := context.Background()
	svc := &Service{}

	if err := svc.SubscribeRoom(ctx, "room-1", "alpha"); err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
		t.Fatalf("SubscribeRoom guard mismatch: %v", err)
	}
	if err := svc.UnsubscribeRoom(ctx, "room-1"); err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
		t.Fatalf("UnsubscribeRoom guard mismatch: %v", err)
	}

	if _, err := svc.IsRoomSubscribed(ctx, "room-1"); err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
		t.Fatalf("IsRoomSubscribed guard mismatch: %v", err)
	}
	if _, err := svc.ListSubscribedRooms(ctx); err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
		t.Fatalf("ListSubscribedRooms guard mismatch: %v", err)
	}
	if err := svc.WarmupSubscriptionCache(ctx); err == nil || !strings.Contains(err.Error(), "membernews repository is nil") {
		t.Fatalf("WarmupSubscriptionCache guard mismatch: %v", err)
	}
}

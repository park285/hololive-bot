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

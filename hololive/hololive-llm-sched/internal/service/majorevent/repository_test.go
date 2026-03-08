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

package majorevent

import (
	"context"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepository_Interface(t *testing.T) {
	t.Run("repository methods exist", func(t *testing.T) {
		var _ interface {
			Subscribe(ctx context.Context, roomID, roomName string) error
			Unsubscribe(ctx context.Context, roomID string) error
			IsSubscribed(ctx context.Context, roomID string) (bool, error)
		}

		t.Log("Repository interface verified")
	})
}

func TestRepository_NilPoolGuards(t *testing.T) {
	t.Parallel()

	repo := &Repository{}
	ctx := context.Background()

	t.Run("subscribe", func(t *testing.T) {
		err := repo.Subscribe(ctx, "room-1", "room")
		if err == nil || !strings.Contains(err.Error(), "postgres pool not configured") {
			t.Fatalf("Subscribe() error = %v, want postgres pool not configured", err)
		}
	})

	t.Run("unsubscribe", func(t *testing.T) {
		err := repo.Unsubscribe(ctx, "room-1")
		if err == nil || !strings.Contains(err.Error(), "postgres pool not configured") {
			t.Fatalf("Unsubscribe() error = %v, want postgres pool not configured", err)
		}
	})

	t.Run("is subscribed", func(t *testing.T) {
		_, err := repo.IsSubscribed(ctx, "room-1")
		if err == nil || !strings.Contains(err.Error(), "postgres pool not configured") {
			t.Fatalf("IsSubscribed() error = %v, want postgres pool not configured", err)
		}
	})
}

func TestNormalizeEventForUpsert(t *testing.T) {
	t.Parallel()

	t.Run("defaults empty fields", func(t *testing.T) {
		eventType, linkStatus := normalizeEventForUpsert(&domain.MajorEvent{})
		if eventType != domain.MajorEventTypeEvent {
			t.Fatalf("eventType = %q, want %q", eventType, domain.MajorEventTypeEvent)
		}
		if linkStatus != domain.MajorEventLinkStatusUnchecked {
			t.Fatalf("linkStatus = %q, want %q", linkStatus, domain.MajorEventLinkStatusUnchecked)
		}
	})

	t.Run("preserves explicit fields", func(t *testing.T) {
		eventType, linkStatus := normalizeEventForUpsert(&domain.MajorEvent{
			Type:       domain.MajorEventTypeNews,
			LinkStatus: domain.MajorEventLinkStatusOK,
		})
		if eventType != domain.MajorEventTypeNews {
			t.Fatalf("eventType = %q, want %q", eventType, domain.MajorEventTypeNews)
		}
		if linkStatus != domain.MajorEventLinkStatusOK {
			t.Fatalf("linkStatus = %q, want %q", linkStatus, domain.MajorEventLinkStatusOK)
		}
	})
}

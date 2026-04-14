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

package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSimpleRenderer_RenderGroupSingle(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)

	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 5,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:  "room-1",
				Channel: &domain.Channel{ID: "channel-id", Name: "시온"},
				Stream: &domain.Stream{
					ID:             "stream-1",
					Title:          "테스트 방송",
					ChannelID:      "channel-id",
					ChannelName:    "시온",
					Status:         domain.StreamStatusUpcoming,
					StartScheduled: &start,
				},
				MinutesUntil: 5,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	if !strings.Contains(message, "시온") {
		t.Fatalf("expected member name in message, got %q", message)
	}
	if !strings.Contains(message, "테스트 방송") {
		t.Fatalf("expected title in message, got %q", message)
	}
}

func TestSimpleRenderer_RenderGroupMultiple_DetailedFormat(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	link1 := "https://youtube.com/watch?v=abc"
	link2 := "https://youtube.com/watch?v=def"

	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 5,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c1", Name: "Member1"},
				Stream:       &domain.Stream{ID: "abc", Title: "Title1", Link: &link1, StartScheduled: &start},
				MinutesUntil: 5,
			},
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c2", Name: "Member2"},
				Stream:       &domain.Stream{ID: "def", Title: "Title2", Link: &link2, StartScheduled: &start},
				MinutesUntil: 5,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	expected := "⏰ 방송 5분 전 알림\n\n" +
		"⏰ Member1 방송 예정\n📺 Title1\n🔗 https://youtube.com/watch?v=abc\n\n" +
		"⏰ Member2 방송 예정\n📺 Title2\n🔗 https://youtube.com/watch?v=def"
	if message != expected {
		t.Errorf("unexpected message:\ngot:  %q\nwant: %q", message, expected)
	}
}

func TestSimpleRenderer_RenderGroupMultiple_KeepsPerItemTimingWhenDifferentFromGroup(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	start := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	link1 := "https://youtube.com/watch?v=abc"
	link2 := "https://youtube.com/watch?v=def"

	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 3,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c1", Name: "Member1"},
				Stream:       &domain.Stream{ID: "abc", Title: "Title1", Link: &link1, StartScheduled: &start},
				MinutesUntil: 3,
			},
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c2", Name: "Member2"},
				Stream:       &domain.Stream{ID: "def", Title: "Title2", Link: &link2, StartScheduled: &start},
				MinutesUntil: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	expected := "⏰ 방송 3분 전 알림\n\n" +
		"⏰ Member1 방송 예정\n📺 Title1\n🔗 https://youtube.com/watch?v=abc\n\n" +
		"⏰ Member2 방송 1분 전\n📺 Title2\n🔗 https://youtube.com/watch?v=def"
	if message != expected {
		t.Fatalf("unexpected message:\ngot:  %q\nwant: %q", message, expected)
	}
}

func TestSimpleRenderer_RenderGroupMultiple(t *testing.T) {
	t.Parallel()

	renderer := NewSimpleRenderer()
	message, err := renderer.RenderGroup(context.Background(), NotificationGroup{
		RoomID:       "room-1",
		MinutesUntil: 3,
		Notifications: []domain.AlarmNotification{
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c1", Name: "멤버1"},
				Stream:       &domain.Stream{ID: "s1", Title: "방송1", ChannelName: "멤버1"},
				MinutesUntil: 3,
			},
			{
				RoomID:       "room-1",
				Channel:      &domain.Channel{ID: "c2", Name: "멤버2"},
				Stream:       &domain.Stream{ID: "s2", Title: "방송2", ChannelName: "멤버2"},
				MinutesUntil: 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderGroup() error = %v", err)
	}

	expected := "⏰ 방송 3분 전 알림\n\n" +
		"⏰ 멤버1 방송 예정\n📺 방송1\n🔗 https://youtube.com/watch?v=s1\n\n" +
		"⏰ 멤버2 방송 1분 전\n📺 방송2\n🔗 https://youtube.com/watch?v=s2"
	if message != expected {
		t.Fatalf("unexpected grouped message:\ngot:  %q\nwant: %q", message, expected)
	}
}
